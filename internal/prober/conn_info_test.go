package prober

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEstimateHops(t *testing.T) {
	tests := []struct {
		ttl    int
		hops   int
		wantOK bool
	}{
		{0, 0, false},
		{-1, 0, false},
		{256, 0, false},
		{64, 0, true},
		{54, 10, true},
		{1, 63, true},
		{65, 63, true},
		{128, 0, true},
		{117, 11, true},
		{129, 126, true},
		{255, 0, true},
		{243, 12, true},
	}
	for _, tt := range tests {
		hops, ok := EstimateHops(tt.ttl)
		if hops != tt.hops || ok != tt.wantOK {
			t.Errorf("EstimateHops(%d) = %d, %v; want %d, %v", tt.ttl, hops, ok, tt.hops, tt.wantOK)
		}
	}
}

// ttlFakeConn reports a fixed TTL for every packet, like icmpConn does with
// control messages enabled.
type ttlFakeConn struct {
	*fakeICMPConn
	ttl int
}

func (c *ttlFakeConn) ReadFromTTL(b []byte) (int, int, net.Addr, error) {
	n, addr, err := c.ReadFrom(b)
	return n, c.ttl, addr, err
}

func icmpReplyTTL(t *testing.T, conn packetConn) int {
	t.Helper()
	p := newTestProber(t, ICMPV4, conn, []byte("fake"), "icmpv4")
	if err := p.Accept("icmpv4://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	r := make(chan *Event, 100)
	done := make(chan error, 1)
	go func() { done <- p.Start(r, time.Hour, time.Second) }()
	defer func() {
		p.Stop()
		<-done
	}()
	for {
		select {
		case e := <-r:
			if e.Result != SUCCESS {
				continue
			}
			if e.Details == nil || e.Details.ICMP == nil {
				t.Fatal("no ICMP details")
			}
			return e.Details.ICMP.ReplyTTL
		case <-time.After(3 * time.Second):
			t.Fatal("no reply")
		}
	}
}

func TestICMPProberReplyTTL(t *testing.T) {
	if got := icmpReplyTTL(t, &ttlFakeConn{fakeICMPConn: newFakeICMPConn(), ttl: 54}); got != 54 {
		t.Errorf("ReplyTTL = %d, want 54", got)
	}
	// A connection without TTL support leaves it unknown
	if got := icmpReplyTTL(t, newFakeICMPConn()); got != 0 {
		t.Errorf("ReplyTTL without TTL support = %d, want 0", got)
	}
}

func TestHTTPProberConnInfoTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	cert := srv.Certificate()

	for _, keepAlive := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep_alive=%v", keepAlive), func(t *testing.T) {
			p := NewHTTPProber(HTTPS, &HTTPConfig{KeepAlive: keepAlive}, "https")
			defer p.Stop()
			// Twice, so that keep_alive reuses the connection
			for i := range 2 {
				e := probeOnce(t, p, "https://"+host)
				if e.Result != SUCCESS {
					t.Fatalf("probe %d: result = %v (%s)", i, e.Result, e.Message)
				}
				h := e.Details.HTTP
				if h.RemoteAddr != host {
					t.Errorf("probe %d: RemoteAddr = %q, want %q", i, h.RemoteAddr, host)
				}
				if h.TLS == nil {
					t.Fatalf("probe %d: no TLS details", i)
				}
				if !strings.HasPrefix(h.TLS.Version, "TLS 1.") {
					t.Errorf("Version = %q", h.TLS.Version)
				}
				if !h.TLS.NotAfter.Equal(cert.NotAfter) {
					t.Errorf("NotAfter = %v, want %v", h.TLS.NotAfter, cert.NotAfter)
				}
				wantIssuer := cert.Issuer.CommonName
				if wantIssuer == "" {
					wantIssuer = cert.Issuer.String()
				}
				if h.TLS.Issuer != wantIssuer || h.TLS.Issuer == "" {
					t.Errorf("Issuer = %q, want %q", h.TLS.Issuer, wantIssuer)
				}
				if h.TLS.SubjectCN != cert.Subject.CommonName {
					t.Errorf("SubjectCN = %q, want %q", h.TLS.SubjectCN, cert.Subject.CommonName)
				}
			}
		})
	}
}

// A response that is received but rejected still reports its peer, so a
// switchover to a peer answering 503 is visible.
func TestHTTPProberConnInfoOnRejectedResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "down")
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	for name, cfg := range map[string]*HTTPConfig{
		"status": {ExpectCodes: "200"},
		"body":   {ExpectBody: "ok"},
	} {
		t.Run(name, func(t *testing.T) {
			e := probeOnce(t, NewHTTPProber(HTTPS, cfg, "https"), "https://"+host)
			if e.Result != FAILED {
				t.Fatalf("result = %v (%s), want FAILED", e.Result, e.Message)
			}
			if e.Details == nil || e.Details.HTTP == nil {
				t.Fatal("rejected response without HTTP details")
			}
			h := e.Details.HTTP
			if h.RemoteAddr != host || h.TLS == nil || h.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("details = %+v", h)
			}
		})
	}
}

func TestHTTPProberConnInfoRedirect(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer final.Close()
	first := httptest.NewServer(http.RedirectHandler(final.URL+"/x", http.StatusFound))
	defer first.Close()

	p := NewHTTPProber(HTTP, &HTTPConfig{}, "http")
	e := probeOnce(t, p, "http://"+strings.TrimPrefix(first.URL, "http://"))
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	if want := strings.TrimPrefix(final.URL, "http://"); e.Details.HTTP.RemoteAddr != want {
		t.Errorf("RemoteAddr = %q, want final server %q", e.Details.HTTP.RemoteAddr, want)
	}
	if e.Details.HTTP.TLS != nil {
		t.Errorf("plain HTTP has TLS details: %+v", e.Details.HTTP.TLS)
	}
}
