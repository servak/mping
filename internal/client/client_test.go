//go:build darwin || linux || freebsd

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/server"
)

var ctx = context.Background()

// shortDir returns a directory for sockets, whose paths must be short.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// serve serves h on a socket.
func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	socket := filepath.Join(shortDir(t), "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return New(socket)
}

// set sets a variable until the end of the test.
func set[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func TestNotRunning(t *testing.T) {
	dir := shortDir(t)
	stale := filepath.Join(dir, "stale")
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	for _, socket := range []string{stale, filepath.Join(dir, "none"), filepath.Join(dir, "none", "none")} {
		_, err := New(socket).Health(ctx)
		if !errors.Is(err, ErrNotRunning) || !strings.Contains(err.Error(), socket) {
			t.Error(err)
		}
		if err := New(socket).Shutdown(ctx, true); !errors.Is(err, ErrNotRunning) {
			t.Error(err)
		}
	}

	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, socket := range []string{filepath.Join(file, "s"), "/tmp/" + strings.Repeat("x", 200)} {
		if _, err := New(socket).Health(ctx); err == nil || errors.Is(err, ErrNotRunning) {
			t.Errorf("%s: %v", socket, err)
		}
	}
}

func TestAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		body    string
		want    string // error
		wantAPI bool
	}{
		"ok":              {200, `{"status":"ok","version":"v1","pid":7}`, "", false},
		"unknown fields":  {200, `{"status":"ok","version":"v1","pid":7,"new":[]}`, "", false},
		"error":           {404, `{"error":{"code":"not_found","message":"no watch"}}`, "no watch", true},
		"no error in it":  {500, `{}`, "unexpected answer", false},
		"error, not JSON": {502, `<html>`, "unexpected answer", false},
		"not JSON":        {200, `hello`, "unexpected answer", false},
		"cut":             {200, `{"status":`, "unexpected answer", false},
		"redirect":        {302, ``, "", false},
		"too large":       {200, `"` + strings.Repeat("x", maxResponse) + `"`, "larger than", false},
	} {
		t.Run(name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(api.VersionHeader, "v1")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			h, err := c.Health(ctx)
			var e *api.Error
			switch {
			case name == "redirect":
				if err == nil {
					t.Error("no error")
				}
				return
			case tc.want == "":
				if err != nil || h != (api.Health{Status: "ok", Version: "v1", PID: 7}) {
					t.Error(h, err)
				}
			case err == nil || !strings.Contains(err.Error(), tc.want):
				t.Error(err)
			case tc.wantAPI != errors.As(err, &e):
				t.Errorf("%#v", err)
			}
			if c.ServerVersion != "v1" {
				t.Errorf("version %q", c.ServerVersion)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	set(t, &timeout, 50*time.Millisecond)
	release := make(chan struct{})
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			time.Sleep(200 * time.Millisecond)
			io.WriteString(w, `{"id":"w-1"}`)
			return
		}
		<-release
	})
	defer close(release)
	if _, err := c.Health(ctx); err == nil || !strings.Contains(err.Error(), "did not answer within 50ms") {
		t.Error(err)
	}
	// Creating one takes longer
	if w, err := c.Create(ctx, api.CreateRequest{}); err != nil || w.ID != "w-1" {
		t.Error(w, err)
	}
	canceled, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := c.Health(canceled); !errors.Is(err, context.DeadlineExceeded) {
		t.Error(err)
	}
}

func TestLost(t *testing.T) {
	c := serve(t, func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	if _, err := c.Create(ctx, api.CreateRequest{}); err == nil || !lost(err) || !strings.Contains(err.Error(), "went away") {
		t.Error(err)
	}
}

// All methods against the real server.
func TestServer(t *testing.T) {
	socket := filepath.Join(shortDir(t), "s")
	run, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- server.Run(run, server.Options{
			Socket: socket, Version: "v1", Logger: slog.New(slog.DiscardHandler),
			LoadConfig: func() (*config.Config, error) { return config.DefaultConfig(), nil },
		})
	}()
	defer stop()
	c := New(socket)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	wantCode := func(err error, code string) {
		t.Helper()
		var e *api.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Errorf("got %v, want %s", err, code)
		}
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		h, err := c.Health(ctx)
		if err == nil {
			if h.PID != os.Getpid() {
				t.Errorf("%+v", h)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
	}
	if !server.Locked(socket) {
		t.Error("not locked")
	}
	w, err := c.Create(ctx, api.CreateRequest{Name: "w", Targets: []string{"tcp://127.0.0.1:1"}, DurationS: 60, IntervalMs: 200, TimeoutMs: 100})
	check(err)
	_, err = c.Create(ctx, api.CreateRequest{Name: "v", Targets: []string{"nope://x"}})
	wantCode(err, api.CodeUnresolvable)
	list, err := c.List(ctx)
	check(err)
	if len(list.Watches) != 1 || list.Watches[0].ID != w.ID {
		t.Errorf("%+v", list)
	}
	detail, err := c.Get(ctx, "w")
	check(err)
	if detail.ID != w.ID || len(detail.Targets) != 1 {
		t.Errorf("%+v", detail)
	}
	_, err = c.Get(ctx, "x/y?force=true#x y%41")
	wantCode(err, api.CodeNotFound)
	if err == nil || !strings.Contains(err.Error(), `"x/y?force=true#x y%41"`) {
		t.Error(err)
	}
	extended, err := c.Extend(ctx, w.ID, 120)
	check(err)
	if !extended.ExpiresAt.After(w.ExpiresAt) {
		t.Errorf("%+v", extended)
	}
	events, err := c.Events(ctx, "w", 1)
	check(err)
	if len(events.Events) == 0 || events.Events[0].Kind != api.EventWatchExtended {
		t.Errorf("%+v", events)
	}
	for _, all := range []bool{false, true} {
		sum, err := c.Summary(ctx, "w", all)
		check(err)
		if sum.ID != w.ID || len(sum.Targets) != 1 {
			t.Errorf("%+v", sum)
		}
	}
	wantCode(c.Delete(ctx, "w", false), api.CodeConflict)
	wantCode(c.Shutdown(ctx, false), api.CodeConflict)
	stopped, err := c.Stop(ctx, "w")
	check(err)
	if stopped.State == api.StateRunning {
		t.Errorf("%+v", stopped)
	}
	check(c.Delete(ctx, "w", true))
	_, err = c.Create(ctx, api.CreateRequest{Name: "x", Targets: []string{"tcp://127.0.0.1:1"}, TimeoutMs: 300, IntervalMs: 200})
	check(err)
	check(c.Shutdown(ctx, true))
	if server.Locked(socket) {
		t.Error("locked")
	}
	check(<-done)
}

// lockSocket holds the lock of a server.
func lockSocket(t *testing.T, socket string) *os.File {
	t.Helper()
	f, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err == nil {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// A server has ended when it has released its lock, or when another process
// answers.
func TestShutdownWaits(t *testing.T) {

	socket := filepath.Join(shortDir(t), "s")
	lock := lockSocket(t, socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{"pid":7}`) })
	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(202)
		io.WriteString(w, "{}")
		ln.Close()
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	srv.SetKeepAlivesEnabled(false)

	set(t, &endTimeout, 150*time.Millisecond)
	c := New(socket)
	if err := c.Shutdown(ctx, false); err == nil || !strings.Contains(err.Error(), "pid 7") {
		t.Error("with the lock held:", err)
	}
	endTimeout = 10 * time.Second
	if ln, err = net.Listen("unix", socket); err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	released := time.Now().Add(300 * time.Millisecond)
	time.AfterFunc(300*time.Millisecond, func() { lock.Close() })
	if err := c.Shutdown(ctx, false); err != nil || time.Now().Before(released) {
		t.Error("with the lock released later:", err)
	}
}

func TestShutdownReplaced(t *testing.T) {
	set(t, &endTimeout, 5*time.Second)
	var pid atomic.Int32
	pid.Store(7)
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			pid.Store(8)
		}
		fmt.Fprintf(w, `{"pid":%d}`, pid.Load())
	})
	lockSocket(t, c.Socket)
	if err := c.Shutdown(ctx, false); err != nil {
		t.Error(err)
	}
}
