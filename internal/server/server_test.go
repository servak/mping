//go:build darwin || linux || freebsd

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/watch"
)

func defaultConfig() (*config.Config, error) { return config.DefaultConfig(), nil }

// socketPath returns a path for a socket, which must be short, in a directory
// that does not exist.
func socketPath(t *testing.T) string {
	dir, err := os.MkdirTemp("", "mp")
	if err == nil && len(dir) > 70 {
		os.Remove(dir)
		dir, err = os.MkdirTemp("/tmp", "mp")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "d", "s.sock")
}

// testServer is a running server with a client.
type testServer struct {
	t      *testing.T
	client *http.Client
	stop   context.CancelFunc
	done   chan struct{} // closed when Run has returned err
	err    error
}

func run(t *testing.T, o Options) *testServer {
	if o.Socket == "" {
		o.Socket = socketPath(t)
	}
	if o.LoadConfig == nil {
		o.LoadConfig = defaultConfig
	}
	o.Version, o.Logger = "v1.2.3", slog.New(slog.DiscardHandler)
	ctx, stop := context.WithCancel(context.Background())
	s := &testServer{t: t, stop: stop, done: make(chan struct{}), client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return new(net.Dialer).DialContext(ctx, "unix", o.Socket)
		},
	}}}
	go func() {
		defer close(s.done)
		s.err = Run(ctx, o)
	}()
	t.Cleanup(func() {
		stop()
		s.client.CloseIdleConnections()
		s.wait()
	})
	waitFor(t, "the server", func() bool {
		return dial(o.Socket) == nil || s.ended()
	})
	return s
}

func (s *testServer) ended() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// wait returns the error of Run.
func (s *testServer) wait() error {
	s.t.Helper()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		s.t.Fatal("the server does not end")
	}
	return s.err
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// do sends a request, with v as body if it is not nil, and decodes the answer.
func (s *testServer) do(method, path string, v any, res any) int {
	s.t.Helper()
	var body io.Reader
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			s.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, "http://mping"+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if v != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(r)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	if v := resp.Header.Get(api.VersionHeader); v != "v1.2.3" {
		s.t.Errorf("%s %s: version %q", method, path, v)
	}
	if res != nil {
		if err := json.Unmarshal(b, res); err != nil {
			s.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func (s *testServer) want(status int, method, path string, v any, res any) {
	s.t.Helper()
	var raw json.RawMessage
	if status != http.StatusNoContent && res == nil {
		res = &raw
	}
	if got := s.do(method, path, v, res); got != status {
		s.t.Errorf("%s %s: status %d, want %d (%s)", method, path, got, status, raw)
	}
}

// tcpServer accepts connections on loopback until it is closed.
func tcpServer(t *testing.T, addr string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return ln
}

func create(name string, targets ...string) api.CreateRequest {
	return api.CreateRequest{Name: name, Targets: targets, IntervalMs: 200, TimeoutMs: 100}
}

func TestEndToEnd(t *testing.T) {
	s := run(t, Options{})
	var health api.Health
	s.want(200, "GET", "/healthz", nil, &health)
	if health != (api.Health{Status: "ok", Version: "v1.2.3"}) {
		t.Errorf("%+v", health)
	}
	ln, steady := tcpServer(t, "127.0.0.1:0"), tcpServer(t, "127.0.0.1:0")
	target := "tcp://" + ln.Addr().String()
	var w api.Watch
	s.want(201, "POST", "/v1/watches", create("switch-update", target, "tcp://"+steady.Addr().String()), &w)
	s.want(409, "POST", "/v1/watches", create("switch-update", target), nil)

	summary := func() (sum api.Summary) {
		s.want(200, "GET", "/v1/watches/switch-update/summary", nil, &sum)
		return sum
	}
	waitFor(t, "answers", func() bool { return summary().Counts.OK == 2 })
	if sum := summary(); len(sum.Targets) != 0 {
		t.Errorf("%+v", sum)
	}
	ln.Close()
	waitFor(t, "the outage", func() bool { return summary().Counts.Down == 1 })
	var events api.Events
	waitFor(t, "its event", func() bool {
		s.want(200, "GET", "/v1/watches/"+w.ID+"/events?after=1", nil, &events)
		return len(events.Events) > 0
	})
	if e := events.Events[0]; events.WatchID != w.ID || events.Next != 2 || e.Seq != 2 || e.Kind != "outage_start" ||
		e.Target != target || e.Outage == nil || !strings.Contains(e.Outage.LastError, "refused") || time.Since(e.Time) > time.Minute {
		t.Errorf("%+v", events)
	}
	ln = tcpServer(t, ln.Addr().String())
	waitFor(t, "the end of the outage", func() bool {
		s.want(200, "GET", "/v1/watches/"+w.ID+"/events?after=2&limit=1", nil, &events)
		return len(events.Events) > 0
	})
	if e := events.Events[0]; e.Kind != "outage_end" || e.Outage.End == nil || e.Outage.EndUnknown || e.Outage.LostProbes < 3 {
		t.Errorf("%+v", e)
	}
	if sum := summary(); sum.Counts != (api.Counts{Degraded: 1, OK: 1}) || sum.Targets[0].OutageCount != 1 || sum.Targets[0].ConsecutiveFailures != 0 {
		t.Errorf("%+v", sum)
	}
	s.want(200, "POST", "/v1/watches/"+w.ID+"/extend", api.ExtendRequest{DurationS: 10800}, &w)
	if d := w.ExpiresAt.Sub(w.StartedAt); d < 3*time.Hour {
		t.Errorf("ends after %v", d)
	}
	var detail api.WatchDetail
	s.want(200, "GET", "/v1/watches/switch-update", nil, &detail)
	if len(detail.Targets) != 2 || detail.Targets[0] != target {
		t.Errorf("%+v", detail)
	}
	s.want(200, "GET", "/v1/watches/switch-update/summary?targets=all", nil, nil)

	s.want(409, "DELETE", "/v1/watches/switch-update", nil, nil)
	ln.Close() // an outage for the end
	waitFor(t, "the outage", func() bool { return summary().Counts.Down == 1 })
	s.want(202, "POST", "/v1/watches/switch-update/stop", nil, &w)
	if w.State != api.StateStopping {
		t.Errorf("%+v", w)
	}
	waitFor(t, "the end", func() bool { return summary().State == api.StateStopped })
	s.want(200, "GET", "/v1/watches/switch-update/events?after=4", nil, &events)
	if n := len(events.Events); n < 2 || !events.Events[n-2].Outage.EndUnknown || events.Events[n-1].Kind != api.EventWatchStopped {
		t.Errorf("%+v", events)
	}
	s.want(200, "GET", "/v1/watches/switch-update/summary", nil, nil)
	var list api.WatchList
	s.want(200, "GET", "/v1/watches", nil, &list)
	if len(list.Watches) != 1 || list.Watches[0].Reason != api.ReasonRequested {
		t.Errorf("%+v", list)
	}
	s.want(204, "DELETE", "/v1/watches/switch-update", nil, nil)
	s.want(404, "GET", "/v1/watches/"+w.ID, nil, nil)

	var body api.ErrorBody
	s.want(422, "POST", "/v1/watches", create("bad", "tcp://127.0.0.1:80", "nope://x", "tcp://noport"), &body)
	if e := body.Error; e.Code != api.CodeUnresolvable || len(e.Targets) != 2 || e.Targets[0].Target != "nope://x" || e.Targets[1].Message == "" {
		t.Errorf("%+v", e)
	}
}

// What is not answered by an endpoint is JSON, too.
func TestBadRequests(t *testing.T) {
	m := watch.NewManager(defaultConfig)
	defer m.Shutdown(context.Background())
	var log bytes.Buffer
	h := newHandler(m, "v", slog.New(slog.NewTextHandler(&log, nil)), func() { t.Error("shutdown") })
	const valid = `{"name":"w","targets":["tcp://127.0.0.1:1"]}`
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/", "", 404},
		{"GET", "/v2/watches", "", 404},
		{"GET", "/v1/watches/", "", 404},
		{"GET", "//v1/watches", "", 404},
		{"GET", "/v1/watches/../watches", "", 404},
		{"GET", "/v1/watches/%2e%2e/summary", "", 404},
		{"GET", "/v1/watches/w/nope", "", 404},
		{"GET", "/v1/watches/w", "", 404},
		{"GET", "/v1/watches/%0a%1b%5b31m%00", "", 404},
		{"GET", "/v1/watches/a%2fb", "", 404},
		{"GET", "/v1/watches/" + strings.Repeat("x", 5000), "", 404},
		{"DELETE", "/v1/watches/w", "", 404},
		{"DELETE", "/v1/watches/x%0aforged=1", "", 404},
		{"POST", "/v1/watches/w/stop", "", 404},
		{"PUT", "/v1/watches", valid, 405},
		{"POST", "/v1/watches/w", "", 405},
		{"GET", "/v1/shutdown", "", 405},
		{"OPTIONS", "/v1/watches", "", 405},
		{"GET", "/healthz?x=1", "", 400},
		{"GET", "/v1/watches?limit=1", "", 400},
		{"GET", "/v1/watches/w?%zz", "", 400},
		{"GET", "/v1/watches/w/events?after=-1", "", 400},
		{"GET", "/v1/watches/w/events?after=", "", 400},
		{"GET", "/v1/watches/w/events?after=9999999999999999999", "", 400},
		{"GET", "/v1/watches/w/events?after=1&after=2", "", 400},
		{"GET", "/v1/watches/w/events?limit=0", "", 400},
		{"GET", "/v1/watches/w/events?limit=10001", "", 400},
		{"GET", "/v1/watches/w/events?limit=10000&after=0", "", 404},
		{"GET", "/v1/watches/w/summary?targets=some", "", 400},
		{"GET", "/v1/watches/w/summary?targets", "", 400},
		{"DELETE", "/v1/watches/w?force=1", "", 400},
		{"DELETE", "/v1/watches/w?force=true", "", 404},
		{"POST", "/v1/watches/w/stop", "{}", 400},
		{"POST", "/v1/watches/w/extend", `{"duration_s":0}`, 400},
		{"POST", "/v1/watches/w/extend", `{"duration_s":60}`, 404},
		{"POST", "/v1/watches/w/extend", `{"duration_s":"1h"}`, 400},
		{"POST", "/v1/watches", "", 400},
		{"POST", "/v1/watches", "null", 400},
		{"POST", "/v1/watches", "[]", 400},
		{"POST", "/v1/watches", valid[:20], 400},
		{"POST", "/v1/watches", valid + "x", 400},
		{"POST", "/v1/watches", valid + valid, 400},
		{"POST", "/v1/watches", `{"nope":1,` + valid[1:], 400},
		{"POST", "/v1/watches", `{"interval_ms":0.5,` + valid[1:], 400},
		{"POST", "/v1/watches", `{"interval_ms":1e400,` + valid[1:], 400},
		{"POST", "/v1/watches", `{"name":"w","targets":["` + strings.Repeat("x", 1<<20) + `"]}`, 413},
		{"POST", "/v1/watches", valid + strings.Repeat(" ", 1<<20), 413},
		{"POST", "/v1/shutdown", "", 400},
		{"POST", "/v1/shutdown", `{"force":1}`, 400},
	} {
		r := httptest.NewRequest(tc.method, "/", strings.NewReader(tc.body))
		u, err := r.URL.Parse("http://mping" + tc.path)
		if strings.HasPrefix(tc.path, "//") || strings.Contains(tc.path, "..") {
			u, err = r.URL.Parse("/")
			u.Path = tc.path
		}
		if err != nil {
			t.Fatal(err)
		}
		r.URL = u
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		checkError(t, h, r, tc.status)
	}
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/json; ;"} {
		r := httptest.NewRequest("POST", "/v1/watches", strings.NewReader(valid))
		r.Header.Set("Content-Type", contentType)
		checkError(t, h, r, 415)
	}
	if l := m.List(); len(l) != 0 {
		t.Errorf("created %+v", l)
	}
	for line := range strings.Lines(log.String()) {
		if !strings.HasPrefix(line, "time=") || strings.ContainsAny(strings.TrimSpace(line), "\n\x1b\x00") {
			t.Errorf("log line %q", line)
		}
	}
	if !strings.Contains(log.String(), `method=DELETE path="/v1/watches/x%0aforged=1" status=404`) || strings.Contains(log.String(), "method=GET") {
		t.Errorf("log:\n%s", log.String())
	}
}

func checkError(t *testing.T, h http.Handler, r *http.Request, status int) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var body api.ErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != status || body.Error == nil ||
		body.Error.Code == "" || body.Error.Message == "" || w.Header().Get(api.VersionHeader) != "v" || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("%s %.80s: got %d, want %d: %s", r.Method, r.URL, w.Code, status, w.Body)
	}
	if allow := w.Header().Get("Allow"); (allow != "") != (status == 405) {
		t.Errorf("%s %s: allow %q", r.Method, r.URL, allow)
	}
}

func TestErrorStatuses(t *testing.T) {
	h := &handler{log: slog.New(slog.DiscardHandler)}
	for code, status := range map[string]int{
		api.CodeInvalid: 400, api.CodeNotFound: 404, api.CodeConflict: 409, api.CodeLimit: 429,
		api.CodeUnresolvable: 422, api.CodeShuttingDown: 503, api.CodeInternal: 500,
	} {
		w := httptest.NewRecorder()
		h.reply(w, httptest.NewRequest("GET", "/", nil), 200, nil, api.Errorf(code, "x"))
		if w.Code != status {
			t.Errorf("%s: %d", code, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.reply(w, httptest.NewRequest("GET", "/", nil), 200, nil, errors.New("x"))
	if w.Code != 500 {
		t.Errorf("other: %d", w.Code)
	}
}

func TestPanic(t *testing.T) {
	var log bytes.Buffer
	h := newHandler(nil, "v", slog.New(slog.NewTextHandler(&log, nil)), nil)
	checkError(t, h, httptest.NewRequest("GET", "/v1/watches", nil), 500)
	if !strings.Contains(log.String(), "panic") {
		t.Errorf("log: %s", &log)
	}
}

func TestShutdownRequest(t *testing.T) {
	socket := socketPath(t)
	s := run(t, Options{Socket: socket})
	target := "tcp://" + tcpServer(t, "127.0.0.1:0").Addr().String()
	s.want(201, "POST", "/v1/watches", create("w", target), nil)
	s.want(409, "POST", "/v1/shutdown", api.ShutdownRequest{}, nil)
	s.want(202, "POST", "/v1/shutdown", api.ShutdownRequest{Force: true}, nil)
	if err := s.wait(); err != nil {
		t.Error(err)
	}
	if _, err := os.Lstat(socket); err == nil {
		t.Error("the socket is left behind")
	}

	s = run(t, Options{Socket: socket})
	s.want(202, "POST", "/v1/shutdown", api.ShutdownRequest{}, nil)
	if err := s.wait(); err != nil {
		t.Error(err)
	}
}

// The end of a server ends a request that creates a watch, and waits for the
// watches.
func TestShutdownWhileCreating(t *testing.T) {
	loading, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s := run(t, Options{LoadConfig: func() (*config.Config, error) {
		once.Do(func() { close(loading); <-proceed })
		return config.DefaultConfig(), nil
	}})
	result := make(chan int)
	go func() { result <- s.do("POST", "/v1/watches", create("w", "tcp://127.0.0.1:1"), nil) }()
	<-loading
	s.stop()
	time.Sleep(50 * time.Millisecond)
	if s.ended() {
		t.Error("the server has not waited")
	}
	close(proceed)
	if status := <-result; status != http.StatusServiceUnavailable {
		t.Errorf("status %d", status)
	}
	if err := s.wait(); err != nil {
		t.Error(err)
	}
}

// Run returns only when the last probes have ended.
func TestShutdownWaitsForProbes(t *testing.T) {
	var active atomic.Int32
	silent := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		<-r.Context().Done()
	}))
	defer silent.Close()
	s := run(t, Options{})
	req := create("w", silent.URL)
	req.TimeoutMs = 1000
	s.want(201, "POST", "/v1/watches", req, nil)
	waitFor(t, "a probe", func() bool { return active.Load() > 0 })
	s.stop()
	if err := s.wait(); err != nil {
		t.Error(err)
	}
	time.Sleep(50 * time.Millisecond) // the server notices later
	if n := active.Load(); n > 0 {
		t.Errorf("%d probes are still running", n)
	}
}

func TestIdleExit(t *testing.T) {
	const idle = 300 * time.Millisecond
	start := time.Now()
	s := run(t, Options{IdleExit: idle})
	if err := s.wait(); err != nil || time.Since(start) < idle {
		t.Errorf("after %v: %v", time.Since(start), err)
	}

	s = run(t, Options{IdleExit: idle})
	s.want(201, "POST", "/v1/watches", create("w", "tcp://127.0.0.1:1"), nil)
	s.want(202, "POST", "/v1/watches/w/stop", nil, nil)
	time.Sleep(2 * idle)
	s.want(204, "DELETE", "/v1/watches/w?force=true", nil, nil)
	start = time.Now()
	if err := s.wait(); err != nil || time.Since(start) < idle/2 {
		t.Errorf("after %v: %v", time.Since(start), err)
	}
}

func TestListen(t *testing.T) {
	ctx := context.Background()
	path, mask := socketPath(t), umask()
	ln, unlock, err := listen(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{
		path:               os.ModeSocket | 0o600,
		path + ".lock":     0o600,
		filepath.Dir(path): os.ModeDir | 0o700,
	} {
		if info, err := os.Lstat(p); err != nil || info.Mode() != want {
			t.Errorf("%s: %v, %v", p, info.Mode(), err)
		}
	}
	if got := umask(); got != mask {
		t.Errorf("the umask is now %#o", got)
	}
	start := time.Now()
	if _, _, err := listen(ctx, path); !errors.Is(err, ErrRunning) || time.Since(start) > time.Second {
		t.Errorf("second server: %v after %v", err, time.Since(start))
	}

	// One that ends is waited for
	ln.Close()
	if _, err := os.Lstat(path); err == nil {
		t.Error("the socket is left behind")
	}
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, _, err := listen(short, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("while it ends: %v", err)
	}
	time.AfterFunc(100*time.Millisecond, unlock)
	ln, unlock, err = listen(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	// A crash leaves the socket behind, but not the lock
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	unlock()
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
	if ln, unlock, err = listen(ctx, path); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	unlock()

	// Without the lock file, the socket still tells
	ln, err = net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := listen(ctx, path); !errors.Is(err, ErrRunning) {
		t.Errorf("socket in use: %v", err)
	}
	ln.Close()

	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listen(ctx, path); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Errorf("a file: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Error("the file was removed")
	}
	long := filepath.Join(filepath.Dir(path), strings.Repeat("x", 100))
	if _, _, err := listen(ctx, long); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("a long path: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 2 { // file and lock
		t.Errorf("left behind: %v", entries)
	}
}

func umask() int {
	old := syscall.Umask(0)
	syscall.Umask(old)
	return old
}

// Of servers that start at the same time one wins.
func TestConcurrentListen(t *testing.T) {
	path := socketPath(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			ln, unlock, err := listen(context.Background(), path)
			if err == nil {
				defer unlock()
				defer ln.Close()
				time.Sleep(500 * time.Millisecond)
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, ErrRunning) {
			t.Error(err)
		}
	}
	if won != 1 {
		t.Errorf("%d servers", won)
	}
}

func TestDefaultSocket(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	if p, err := DefaultSocket(); err != nil || p != "/home/u/.mping/server.sock" {
		t.Error(p, err)
	}
	// It would depend on the directory of each process
	t.Setenv("HOME", "home/u")
	if p, err := DefaultSocket(); err == nil {
		t.Error(p)
	}
	t.Setenv("HOME", "")
	if p, err := DefaultSocket(); err == nil {
		t.Error(p)
	}
}
