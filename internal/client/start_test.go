//go:build darwin || linux || freebsd

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/server"
)

// TestMain is also the server that startServer starts, which is this program.
// No test gets near the files of the user, whatever it does wrong.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "server" {
		serverMain()
		return
	}
	os.Setenv("HOME", "/dev/null/home")
	os.Exit(m.Run())
}

func serverMain() {
	home, _ := os.UserHomeDir()
	if msg, err := os.ReadFile(filepath.Join(home, "fail")); err == nil {
		fmt.Fprint(os.Stderr, string(msg))
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // should the test die
	defer cancel()
	if fd, err := os.ReadFile(filepath.Join(home, "facts")); err == nil {
		cwd, _ := os.Getwd()
		sid, _ := unix.Getsid(0)
		n, _ := strconv.Atoi(string(fd))
		var st syscall.Stat_t
		b, _ := json.Marshal(map[string]any{
			"args": os.Args[1:], "cwd": cwd, "sid": sid, "pid": os.Getpid(),
			"inherited": syscall.Fstat(n, &st) == nil,
		})
		os.WriteFile(filepath.Join(home, "facts"), b, 0o600)
	}
	if err := server.Run(ctx, server.Options{
		Version: "v1", Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		LoadConfig: func() (*config.Config, error) { return config.DefaultConfig(), nil },
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newHome sets HOME to a new directory and returns it with a client for the
// default socket, whose server is ended after the test.
func newHome(t *testing.T) (string, *Client) {
	t.Helper()
	home := shortDir(t)
	t.Setenv("HOME", home)
	socket, err := server.DefaultSocket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { New(socket).Shutdown(ctx, true) })
	return home, New(socket)
}

func request1(name string) api.CreateRequest {
	return api.CreateRequest{Name: name, Targets: []string{"tcp://127.0.0.1:1"}, TimeoutMs: 100}
}

func TestAutoStart(t *testing.T) {
	home, c := newHome(t)
	fd, err := syscall.Dup(int(os.Stderr.Fd())) // without close-on-exec, as from a parent
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	facts := filepath.Join(home, "facts")
	if err := os.WriteFile(facts, []byte(strconv.Itoa(fd)), 0o600); err != nil {
		t.Fatal(err)
	}

	var pids []int
	started := func(pid int) { pids = append(pids, pid) }
	if _, err := c.CreateAutoStart(ctx, request1("a"), started); err != nil {
		t.Fatal(err)
	}
	h, err := c.Health(ctx)
	if err != nil || len(pids) != 1 || pids[0] != h.PID {
		t.Errorf("started %v, %+v, %v", pids, h, err)
	}
	if _, err := c.CreateAutoStart(ctx, request1("b"), started); err != nil || len(pids) != 1 {
		t.Error(pids, err)
	}
	b, err := os.ReadFile(facts)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(`{"args":["server","--idle-exit","10m"],"cwd":"/","inherited":false,"pid":%d,"sid":%[1]d}`, h.PID); string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	for path, mode := range map[string]os.FileMode{"": os.ModeDir | 0o700, "server.log": 0o600, "server.sock": os.ModeSocket | 0o600} {
		info, err := os.Stat(filepath.Join(home, ".mping", path))
		if err != nil || info.Mode() != mode {
			t.Errorf("%s: %v %v", path, info.Mode(), err)
		}
	}
	if log, err := os.ReadFile(filepath.Join(home, ".mping", "server.log")); err != nil || !strings.Contains(string(log), "listening") {
		t.Errorf("%s %v", log, err)
	}
}

// However many start at once, one server is left and all watches are on it.
func TestAutoStartConcurrently(t *testing.T) {
	_, c := newHome(t)
	const n = 10
	var started atomic.Int32
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, err := New(c.Socket).CreateAutoStart(ctx, request1(fmt.Sprint("w", i)), func(int) { started.Add(1) })
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	list, err := c.List(ctx)
	if err != nil || len(list.Watches) != n {
		t.Errorf("%d watches, %v", len(list.Watches), err)
	}
	if n := started.Load(); n != 1 {
		t.Errorf("%d have started the server", n)
	}
}

func TestAutoStartFails(t *testing.T) {
	home, c := newHome(t)
	dir := filepath.Join(home, ".mping")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "server.log")
	if err := os.WriteFile(log, []byte("of an older one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var msg strings.Builder
	for i := range 20 {
		fmt.Fprintf(&msg, "line %02d\x1b[31m\n", i)
	}
	if err := os.WriteFile(filepath.Join(home, "fail"), []byte(msg.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := c.CreateAutoStart(ctx, request1("a"), func(int) { t.Error("started") })
	if err == nil {
		t.Fatal("no error")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "the server did not start (see "+log+")\n  line 10[31m\n") || !strings.HasSuffix(got, "\n  line 19[31m") {
		t.Errorf("%q", got)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}

	// Its output alone, and the log that has become too large is emptied
	if err := os.WriteFile(filepath.Join(home, "fail"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil || !strings.HasSuffix(err.Error(), ")\n  no") {
		t.Errorf("%q", err)
	}
	if b, _ := os.ReadFile(log); !strings.HasPrefix(string(b), "of an older one\n") {
		t.Errorf("%q", b)
	}
	if err := os.WriteFile(log, make([]byte, maxLog+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil {
		t.Error("no error")
	}
	if b, _ := os.ReadFile(log); string(b) != "no" {
		t.Errorf("%d bytes", len(b))
	}

	os.Remove(log)
	if err := os.Symlink(filepath.Join(home, "elsewhere"), log); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil || !strings.Contains(err.Error(), "server.log") {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(home, "elsewhere")); err == nil {
		t.Error("the log is behind a link")
	}
}

// An error of the server is no reason to start one.
func TestAutoStartError(t *testing.T) {
	var requests atomic.Int32
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(api.ErrorBody{Error: api.Errorf(api.CodeConflict, "taken")})
	})
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil || err.Error() != "taken" || requests.Load() != 1 {
		t.Error(requests.Load(), err)
	}
}

// A server that is ending is waited for, then the next one is started.
func TestAutoStartAfterEnd(t *testing.T) {
	home, c := newHome(t)
	if err := os.Mkdir(filepath.Join(home, ".mping"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock := lockSocket(t, c.Socket)
	ln, err := net.Listen("unix", c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	var refused atomic.Int32
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if refused.Add(1) == 3 {
			ln.Close()
			time.AfterFunc(300*time.Millisecond, func() { lock.Close() })
		}
		w.WriteHeader(503)
		json.NewEncoder(w).Encode(api.ErrorBody{Error: api.Errorf(api.CodeShuttingDown, "ending")})
	})}
	srv.SetKeepAlivesEnabled(false)
	go srv.Serve(ln)
	defer srv.Close()
	pid := 0
	w, err := c.CreateAutoStart(ctx, request1("a"), func(p int) { pid = p })
	if err != nil || w.Name != "a" || pid == 0 || pid == os.Getpid() || refused.Load() != 3 {
		t.Error(w, err, pid, refused.Load())
	}
	// Those who would have failed did not even try
	if b, _ := os.ReadFile(filepath.Join(home, ".mping", "server.log")); strings.Count(string(b), "\n") != 2 {
		t.Errorf("%s", b)
	}
}

// A server that keeps being lost is not started forever.
func TestAutoStartLost(t *testing.T) {
	var requests atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requests.Add(1)
			panic(http.ErrAbortHandler)
		}
		fmt.Fprint(w, "{}")
	})
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil || requests.Load() != maxStarts+1 {
		t.Error(requests.Load(), err)
	}

	// Nor is one waited for forever
	set(t, &endTimeout, 200*time.Millisecond)
	home, c := newHome(t)
	if err := os.Mkdir(filepath.Join(home, ".mping"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock := lockSocket(t, c.Socket)
	defer lock.Close()
	if _, err := c.CreateAutoStart(ctx, request1("a"), nil); err == nil || !strings.Contains(err.Error(), "does not listen") {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".mping", "server.log")); err == nil {
		t.Error("another server was started")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.CreateAutoStart(canceled, request1("a"), nil); err == nil {
		t.Error("no error")
	}
}
