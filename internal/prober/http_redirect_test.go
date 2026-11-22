package prober

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// redirectSite answers for every host name: ports 443 and 8443 speak TLS, all
// others plain HTTP. It records each request and answers the n-th one with a
// redirect to locations[n], or with 200 once they are used up.
type redirectSite struct {
	plain, tls *httptest.Server
	locations  []string

	mu     sync.Mutex
	dialed map[string]string // client address -> dialed host:port
	reqs   []siteRequest
}

type siteRequest struct {
	addr   string // scheme://host:port as dialed, whatever the Host header
	host   string // Host header
	sni    string
	header http.Header
}

func newRedirectSite(t *testing.T, locations ...string) *redirectSite {
	t.Helper()
	s := &redirectSite{locations: locations, dialed: make(map[string]string)}
	s.plain = httptest.NewServer(s)
	s.tls = httptest.NewTLSServer(s)
	t.Cleanup(s.plain.Close)
	t.Cleanup(s.tls.Close)
	return s
}

func (s *redirectSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := siteRequest{host: r.Host, header: r.Header.Clone()}
	scheme := "http://"
	if r.TLS != nil {
		scheme, req.sni = "https://", r.TLS.ServerName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	req.addr = scheme + s.dialed[r.RemoteAddr]
	s.reqs = append(s.reqs, req)
	if n := len(s.reqs); n <= len(s.locations) {
		w.Header().Set("Location", s.locations[n-1])
		w.WriteHeader(http.StatusFound)
	}
}

func (s *redirectSite) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	srv := s.plain
	if strings.HasSuffix(addr, ":443") || strings.HasSuffix(addr, ":8443") {
		srv = s.tls
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialed[conn.LocalAddr().String()] = addr
	return conn, nil
}

// probe probes target once with the given headers and returns the requests
// the site received.
func (s *redirectSite) probe(t *testing.T, headers map[string]string, target string) []siteRequest {
	t.Helper()
	scheme, _, _ := strings.Cut(target, "://")
	p := NewHTTPProber(ProbeType(scheme), &HTTPConfig{Header: headers}, scheme)
	ct := p.client.Transport.(*customTransport)
	for _, rt := range []http.RoundTripper{ct.transport, ct.hostTransport} {
		if tr, ok := rt.(*http.Transport); ok {
			tr.DialContext = s.dial
		}
	}
	if e := probeOnce(t, p, target); e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs
}

// Configured headers may carry credentials: they must reach the probed origin
// (and its http -> https upgrade) but no other redirect target.
func TestHTTPProberRedirectHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer dummy",
		"Cookie":        "session=dummy",
		"User-Agent":    "mping-dummy",
		"x-token":       "dummy",
	}
	type hop struct {
		location string
		addr     string // where the request must arrive
		sent     bool   // with the configured headers
	}
	tests := []struct {
		name   string
		target string
		hops   []hop
	}{
		{"same host", "http://a.test", []hop{{"http://a.test/next", "http://a.test:80", true}}},
		{"same host, relative", "http://a.test", []hop{{"/next", "http://a.test:80", true}}},
		{"same host, explicit port", "http://a.test", []hop{{"http://a.test:80/next", "http://a.test:80", true}}},
		{"same host, other case", "http://a.test", []hop{{"http://A.Test/next", "http://A.Test:80", true}}},
		{"same host, non-default port", "http://a.test:8080", []hop{{"http://a.test:8080/next", "http://a.test:8080", true}}},
		{"same host, https", "https://a.test", []hop{{"https://a.test/next", "https://a.test:443", true}}},

		{"cross host", "http://127.0.0.1", []hop{{"http://localhost/", "http://localhost:80", false}}},
		{"cross host, https", "https://a.test", []hop{{"https://b.test/", "https://b.test:443", false}}},
		{"cross host, upgrade", "http://a.test", []hop{{"https://b.test/", "https://b.test:443", false}}},
		{"cross host, subdomain", "http://a.test", []hop{{"http://sub.a.test/", "http://sub.a.test:80", false}}},
		{"cross host, several hops", "http://a.test", []hop{
			{"http://b.test/", "http://b.test:80", false},
			{"/next", "http://b.test:80", false},
			{"https://b.test/", "https://b.test:443", false},
		}},
		{"cross host and back", "http://a.test", []hop{
			{"http://b.test/", "http://b.test:80", false},
			{"http://a.test/", "http://a.test:80", true},
		}},

		{"upgrade", "http://a.test", []hop{{"https://a.test/", "https://a.test:443", true}}},
		{"upgrade, explicit ports", "http://a.test:80", []hop{{"https://a.test:443/", "https://a.test:443", true}}},
		{"upgrade, several hops", "http://a.test", []hop{
			{"https://a.test/", "https://a.test:443", true},
			{"/next", "https://a.test:443", true},
		}},
		{"upgrade to non-default port", "http://a.test", []hop{{"https://a.test:8443/", "https://a.test:8443", false}}},
		{"upgrade from non-default port", "http://a.test:8080", []hop{{"https://a.test/", "https://a.test:443", false}}},

		{"downgrade", "https://a.test", []hop{{"http://a.test/", "http://a.test:80", false}}},
		{"downgrade and back", "https://a.test", []hop{
			{"http://a.test/", "http://a.test:80", false},
			{"https://a.test/", "https://a.test:443", true},
		}},

		{"other port", "http://a.test", []hop{{"http://a.test:8080/", "http://a.test:8080", false}}},
		{"other port, https", "https://a.test", []hop{{"https://a.test:8443/", "https://a.test:8443", false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := []hop{{sent: true}} // the probed target itself
			var locations []string
			for _, h := range tt.hops {
				want = append(want, h)
				locations = append(locations, h.location)
			}
			got := newRedirectSite(t, locations...).probe(t, headers, tt.target)
			if len(got) != len(want) {
				t.Fatalf("site received %d requests, want %d: %+v", len(got), len(want), got)
			}
			for i, w := range want {
				if i > 0 && got[i].addr != w.addr {
					t.Errorf("request %d arrived at %s, want %s", i, got[i].addr, w.addr)
				}
				for k, v := range headers {
					if sent := got[i].header.Get(k) == v; sent != w.sent {
						t.Errorf("request %d (%s): header %s sent = %v, want %v", i, got[i].addr, k, sent, w.sent)
					}
				}
			}
		})
	}
}

// Same-port scheme changes cannot be served by redirectSite.
func TestKeepsHeaders(t *testing.T) {
	tests := []struct {
		orig, target string
		want         bool
	}{
		{"http://h:8080/", "https://h:8080/", true},
		{"http://h:443/", "https://h/", true},
		{"https://h:8443/", "http://h:8443/", false},
		{"https://h/", "http://h:443/", false},
		{"http://[2001:db8::1]/", "https://[2001:db8::1]/", true},
		{"http://[2001:db8::1]/", "https://[2001:db8::2]/", false},
		{"https://h/", "https://h./", false},
	}
	for _, tt := range tests {
		orig, err := url.Parse(tt.orig)
		if err != nil {
			t.Fatal(err)
		}
		target, err := url.Parse(tt.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := keepsHeaders(orig, target); got != tt.want {
			t.Errorf("keepsHeaders(%s, %s) = %v, want %v", tt.orig, tt.target, got, tt.want)
		}
	}
}

// The Host override (and SNI) follows host[:port] as written in the URL and is
// independent of the scheme.
func TestHTTPProberHostOverrideRedirects(t *testing.T) {
	const vhost = "vhost.test"
	headers := map[string]string{"Host": vhost, "Authorization": "Bearer dummy"}
	tests := []struct {
		name, target, location string
		wantHost, wantSNI      string
	}{
		{"same host", "http://a.test", "http://a.test/next", vhost, ""},
		{"same host, https", "https://a.test", "https://a.test/next", vhost, vhost},
		{"upgrade", "http://a.test", "https://a.test/", vhost, vhost},
		{"downgrade", "https://a.test", "http://a.test/", vhost, ""},
		{"cross host", "https://a.test", "https://b.test/", "b.test", "b.test"},
		{"other port", "https://a.test", "https://a.test:8443/", "a.test:8443", "a.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newRedirectSite(t, tt.location).probe(t, headers, tt.target)
			if len(got) != 2 {
				t.Fatalf("site received %d requests, want 2: %+v", len(got), got)
			}
			if got[0].host != vhost {
				t.Errorf("first request: Host = %q, want %q", got[0].host, vhost)
			}
			if got[1].host != tt.wantHost || got[1].sni != tt.wantSNI {
				t.Errorf("redirect: Host = %q, SNI = %q, want %q, %q", got[1].host, got[1].sni, tt.wantHost, tt.wantSNI)
			}
		})
	}
}
