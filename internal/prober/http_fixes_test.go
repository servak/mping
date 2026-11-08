package prober

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// An endless body must not be read into memory: the probe ends once the
// counting cap is reached and the size is flagged as a lower bound.
func TestHTTPProberBodyLimit(t *testing.T) {
	chunk := []byte(strings.Repeat("x", 32*1024))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	p := NewHTTPProber(HTTP, &HTTPConfig{}, "http")
	e := probeOnce(t, p, "http://"+strings.TrimPrefix(srv.URL, "http://"))
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s), want SUCCESS", e.Result, e.Message)
	}
	if h := e.Details.HTTP; h.ResponseSize != maxCountBytes || !h.SizeTruncated {
		t.Errorf("ResponseSize = %d truncated = %v, want %d true", h.ResponseSize, h.SizeTruncated, int64(maxCountBytes))
	}
}

// A stalled body hitting Client.Timeout is a TIMEOUT, not a FAILED.
func TestHTTPProberBodyReadTimeout(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(done)

	p := NewHTTPProber(HTTP, &HTTPConfig{}, "http")
	if err := p.Accept("http://" + strings.TrimPrefix(srv.URL, "http://")); err != nil {
		t.Fatal(err)
	}
	p.client.Timeout = 200 * time.Millisecond
	ch := make(chan *Event, 4)
	go p.probe(ch, "http://"+strings.TrimPrefix(srv.URL, "http://"))
	for {
		select {
		case e := <-ch:
			if e.Result == SENT {
				continue
			}
			if e.Result != TIMEOUT {
				t.Fatalf("result = %v (%s), want TIMEOUT", e.Result, e.Message)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("no result")
		}
	}
}

// With a Host header override the TLS SNI must carry that name.
func TestHTTPProberHostOverrideSNI(t *testing.T) {
	var mu sync.Mutex
	var sni string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		sni = h.ServerName
		mu.Unlock()
		return nil, nil
	}}
	srv.StartTLS()
	defer srv.Close()

	cfg := &HTTPConfig{Header: map[string]string{"Host": "vhost.example.test:8443"}}
	p := NewHTTPProber(HTTPS, cfg, "https")
	e := probeOnce(t, p, "https://"+strings.TrimPrefix(srv.URL, "https://"))
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	mu.Lock()
	defer mu.Unlock()
	if sni != "vhost.example.test" {
		t.Errorf("SNI = %q, want vhost.example.test", sni)
	}
}

func countConns(t *testing.T, cfg *HTTPConfig, probes int) int32 {
	t.Helper()
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	p := NewHTTPProber(HTTP, cfg, "http")
	target := "http://" + strings.TrimPrefix(srv.URL, "http://")
	for range probes {
		if e := probeOnce(t, p, target); e.Result != SUCCESS {
			t.Fatalf("result = %v (%s)", e.Result, e.Message)
		}
	}
	p.Stop()
	return conns.Load()
}

func TestHTTPProberKeepAlive(t *testing.T) {
	if got := countConns(t, &HTTPConfig{}, 3); got != 3 {
		t.Errorf("default: %d connections for 3 probes, want 3", got)
	}
	if got := countConns(t, &HTTPConfig{KeepAlive: true}, 3); got != 1 {
		t.Errorf("keep_alive: %d connections for 3 probes, want 1", got)
	}
}

// Stop must close idle keep-alive connections.
func TestHTTPProberStopClosesIdleConns(t *testing.T) {
	var closed atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	p := NewHTTPProber(HTTP, &HTTPConfig{KeepAlive: true}, "http")
	if e := probeOnce(t, p, "http://"+strings.TrimPrefix(srv.URL, "http://")); e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	if closed.Load() != 0 {
		t.Fatal("connection closed before Stop")
	}
	p.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if closed.Load() != 1 {
		t.Errorf("closed conns after Stop = %d, want 1", closed.Load())
	}
}

func bigBodyServer(t *testing.T, size int, chunked bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !chunked {
			w.Header().Set("Content-Length", strconv.Itoa(size))
		}
		buf := []byte(strings.Repeat("a", 64<<10))
		for left := size; left > 0; {
			n := min(left, len(buf))
			w.Write(buf[:n])
			left -= n
		}
	}))
	t.Cleanup(srv.Close)
	return "http://" + strings.TrimPrefix(srv.URL, "http://")
}

func TestHTTPProberResponseSize(t *testing.T) {
	const size = 3 << 20
	for _, chunked := range []bool{false, true} {
		e := probeOnce(t, NewHTTPProber(HTTP, &HTTPConfig{}, "http"), bigBodyServer(t, size, chunked))
		if e.Result != SUCCESS {
			t.Fatalf("result = %v (%s)", e.Result, e.Message)
		}
		if got := e.Details.HTTP.ResponseSize; got != size {
			t.Errorf("chunked=%v: ResponseSize = %d, want %d", chunked, got, size)
		}
		if e.Details.HTTP.SizeTruncated {
			t.Errorf("chunked=%v: SizeTruncated set for a countable body", chunked)
		}
	}

	// Beyond maxCountBytes the size is a lower bound, flagged as such.
	e := probeOnce(t, NewHTTPProber(HTTP, &HTTPConfig{}, "http"), bigBodyServer(t, maxCountBytes+(1<<20), true))
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s)", e.Result, e.Message)
	}
	if h := e.Details.HTTP; h.ResponseSize != maxCountBytes || !h.SizeTruncated {
		t.Errorf("size = %d truncated = %v, want %d true", h.ResponseSize, h.SizeTruncated, maxCountBytes)
	}
}

func TestHTTPProberTruncatedBodyMessage(t *testing.T) {
	cfg := &HTTPConfig{ExpectBody: "nope"}
	e := probeOnce(t, NewHTTPProber(HTTP, cfg, "http"), bigBodyServer(t, 2<<20, false))
	if e.Result != FAILED || !strings.Contains(e.Message, "truncated at 1 MiB") {
		t.Errorf("result = %v msg = %q, want failure mentioning truncation", e.Result, e.Message)
	}
	e = probeOnce(t, NewHTTPProber(HTTP, cfg, "http"), bigBodyServer(t, 10, false))
	if e.Result != FAILED || strings.Contains(e.Message, "truncated") {
		t.Errorf("small body: result = %v msg = %q", e.Result, e.Message)
	}
}

// The Host override (and its SNI) must not follow a redirect to another host.
func TestHTTPProberHostOverrideNotAppliedAfterRedirect(t *testing.T) {
	var mu sync.Mutex
	var snis []string
	other := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host=%s", r.Host)
	}))
	other.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		snis = append(snis, h.ServerName)
		mu.Unlock()
		return nil, nil
	}}
	other.StartTLS()
	defer other.Close()

	// "localhost" and "127.0.0.1" are different hosts for the redirect check.
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	}))
	defer first.Close()

	cfg := &HTTPConfig{
		Header:     map[string]string{"Host": "vhost.example.test"},
		ExpectBody: "host=" + strings.TrimPrefix(other.URL, "https://"),
	}
	e := probeOnce(t, NewHTTPProber(HTTP, cfg, "http"), "http://"+strings.TrimPrefix(first.URL, "http://"))
	if e.Result != SUCCESS {
		t.Fatalf("result = %v (%s): Host override leaked to the redirect target", e.Result, e.Message)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range snis {
		if s == "vhost.example.test" {
			t.Errorf("SNI %q leaked to the redirect target", s)
		}
	}
}

func TestHostOverrideDeterministic(t *testing.T) {
	h := map[string]string{"host": "b.example", "Host": "a.example", "X-A": "1"}
	for range 20 {
		if got := hostOverride(h); got != "a.example" {
			t.Fatalf("hostOverride = %q, want a.example", got)
		}
	}
	if err := (&HTTPConfig{Header: h}).Validate(); err == nil {
		t.Error("Validate accepted two Host entries")
	}
	if err := (&HTTPConfig{Header: map[string]string{"host": "a"}}).Validate(); err != nil {
		t.Errorf("Validate rejected a single Host: %v", err)
	}
}
