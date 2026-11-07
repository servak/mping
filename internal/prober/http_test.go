package prober

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPProberResolveURL(t *testing.T) {
	withTLS := &HTTPConfig{TLS: &TLSConfig{}}
	plain := &HTTPConfig{}

	tests := []struct {
		name      string
		probeType ProbeType
		config    *HTTPConfig
		prefix    string
		target    string
		want      string
	}{
		{"https prefix without tls block", HTTP, plain, "https", "https://example.com", "https://example.com"},
		{"https prefix with probe https", HTTPS, plain, "https", "https://example.com/x", "https://example.com/x"},
		{"http prefix ignores tls block", HTTP, withTLS, "http", "http://example.com", "http://example.com"},
		{"http prefix ignores probe https", HTTPS, plain, "http", "http://example.com", "http://example.com"},
		{"custom prefix explicit https", HTTP, plain, "api-check", "api-check://https://example.com/health", "https://example.com/health"},
		{"custom prefix explicit http overrides tls", HTTPS, withTLS, "api-check", "api-check://http://example.com", "http://example.com"},
		{"custom prefix probe https", HTTPS, plain, "api-check", "api-check://example.com", "https://example.com"},
		{"custom prefix tls block (backward compat)", HTTP, withTLS, "my-https", "my-https://example.com", "https://example.com"},
		{"custom prefix default http", HTTP, plain, "my-http", "my-http://example.com:8080", "http://example.com:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewHTTPProber(tt.probeType, tt.config, tt.prefix)
			if got := p.convertToActualURL(tt.target); got != tt.want {
				t.Errorf("convertToActualURL(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestHTTPProberAcceptRejectsMalformed(t *testing.T) {
	tests := []struct {
		prefix string
		target string
	}{
		{"https", "https://"},
		{"http", "http://https://example.com"},
		{"api-check", "api-check://https://"},
		{"api-check", "api-check://ftp://example.com"},
		{"api-check", "api-check://https://http://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			p := NewHTTPProber(HTTP, &HTTPConfig{}, tt.prefix)
			if err := p.Accept(tt.target); err == nil {
				t.Errorf("Accept(%q) should fail", tt.target)
			}
		})
	}
}

func TestTLSConfigShouldSkipVerify(t *testing.T) {
	var nilTLS *TLSConfig
	if !nilTLS.ShouldSkipVerify() {
		t.Error("nil TLSConfig should default to skip verify")
	}
	if !(&TLSConfig{}).ShouldSkipVerify() {
		t.Error("empty TLSConfig should default to skip verify")
	}
	if (&TLSConfig{SkipVerify: BoolPtr(false)}).ShouldSkipVerify() {
		t.Error("explicit skip_verify: false should verify")
	}
}

// probeOnce runs a single probe and returns the final (non-SENT) event.
func probeOnce(t *testing.T, p *HTTPProber, target string) *Event {
	t.Helper()
	if err := p.Accept(target); err != nil {
		t.Fatalf("Accept(%q): %v", target, err)
	}
	p.client.Timeout = 3 * time.Second
	ch := make(chan *Event, 4)
	p.wg.Add(1)
	go p.probe(ch, target)
	for {
		select {
		case e := <-ch:
			if e.Result == SENT {
				continue
			}
			return e
		case <-time.After(5 * time.Second):
			t.Fatalf("no result for %s", target)
		}
	}
}

func TestHTTPProberProbeScheme(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer tlsSrv.Close()
	tlsHost := strings.TrimPrefix(tlsSrv.URL, "https://")

	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer plainSrv.Close()
	plainHost := strings.TrimPrefix(plainSrv.URL, "http://")

	tests := []struct {
		name      string
		probeType ProbeType
		config    *HTTPConfig
		prefix    string
		target    string
		want      reason
		wantType  string
	}{
		{"https entry without tls block", HTTP, &HTTPConfig{ExpectCodes: "200"}, "https", "https://" + tlsHost, SUCCESS, "https"},
		{"https entry with empty tls block skips verify", HTTP, &HTTPConfig{TLS: &TLSConfig{}}, "https", "https://" + tlsHost, SUCCESS, "https"},
		{"https entry with skip_verify false verifies", HTTP, &HTTPConfig{TLS: &TLSConfig{SkipVerify: BoolPtr(false)}}, "https", "https://" + tlsHost, FAILED, ""},
		{"probe https without tls block", HTTPS, &HTTPConfig{}, "secure", "secure://" + tlsHost, SUCCESS, "https"},
		{"custom prefix with explicit https", HTTP, &HTTPConfig{ExpectCodes: "200,201"}, "api-check", "api-check://https://" + tlsHost, SUCCESS, "https"},
		{"custom prefix plain http", HTTP, &HTTPConfig{ExpectCodes: "200,201"}, "api-check", "api-check://" + plainHost, SUCCESS, "http"},
		{"http entry with tls block stays http", HTTP, &HTTPConfig{TLS: &TLSConfig{SkipVerify: BoolPtr(true)}}, "http", "http://" + plainHost, SUCCESS, "http"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewHTTPProber(tt.probeType, tt.config, tt.prefix)
			e := probeOnce(t, p, tt.target)
			if e.Result != tt.want {
				t.Fatalf("result = %v (%s), want %v", e.Result, e.Message, tt.want)
			}
			if tt.want != SUCCESS {
				return
			}
			if e.Details == nil || e.Details.HTTP == nil {
				t.Fatal("expected HTTP details")
			}
			if e.Details.ProbeType != tt.wantType {
				t.Errorf("ProbeType = %q, want %q", e.Details.ProbeType, tt.wantType)
			}
			if len(e.Details.HTTP.Redirects) != 0 {
				t.Errorf("unexpected redirects: %v", e.Details.HTTP.Redirects)
			}
		})
	}
}

func TestHTTPProberRedirectDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	p := NewHTTPProber(HTTP, &HTTPConfig{}, "api-check")
	e := probeOnce(t, p, "api-check://"+host+"/old")
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	got := e.Details.HTTP.Redirects
	if len(got) != 1 || got[0] != srv.URL+"/new" {
		t.Errorf("Redirects = %v, want [%s/new]", got, srv.URL)
	}
}

func TestHTTPProberHostHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host=%s ua=%s", r.Host, r.Header.Get("User-Agent"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	cfg := &HTTPConfig{
		Header:     map[string]string{"host": "vhost.example.test", "User-Agent": "mping-test"},
		ExpectBody: "host=vhost.example.test ua=mping-test",
	}
	p := NewHTTPProber(HTTP, cfg, "http")
	e := probeOnce(t, p, "http://"+host)
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s), Host header not applied", e.Result, e.Message)
	}
}

type recordingTransport struct{ got *http.Request }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func TestCustomTransportDoesNotMutateRequest(t *testing.T) {
	rt := &recordingTransport{}
	ct := &customTransport{transport: rt, headers: map[string]string{"Host": "vhost", "X-Test": "1"}}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	if _, err := ct.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if req.Host != "example.com" || req.Header.Get("X-Test") != "" {
		t.Errorf("original request was mutated: host=%q headers=%v", req.Host, req.Header)
	}
	if rt.got.Host != "vhost" || rt.got.Header.Get("X-Test") != "1" {
		t.Errorf("sent request: host=%q headers=%v", rt.got.Host, rt.got.Header)
	}
	if _, ok := rt.got.Header["Host"]; ok {
		t.Error("Host should be set via req.Host, not Header")
	}
}
