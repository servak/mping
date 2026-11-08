package prober

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// An endless body must not be read into memory: the probe ends once the
// size cap is reached.
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
	if got := e.Details.HTTP.ResponseSize; got != maxBodyBytes {
		t.Errorf("ResponseSize = %d, want %d", got, int64(maxBodyBytes))
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
