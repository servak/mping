//go:build e2e && (darwin || linux || freebsd)

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// watchHome is a user of mping with a home directory, and so a server, of their
// own.
type watchHome struct {
	t    *testing.T
	home string

	mu   sync.Mutex
	pids []int // of servers
}

func newWatchHome(t *testing.T) *watchHome {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "mp") // paths of sockets must be short
	if err != nil {
		t.Fatal(err)
	}
	h := &watchHome{t: t, home: home}
	t.Cleanup(func() {
		h.run("server", "stop", "-f")
		for _, pid := range h.pids {
			if syscall.Kill(pid, syscall.SIGKILL) == nil && !h.gone(pid) {
				t.Errorf("server %d was left", pid)
			}
		}
		os.RemoveAll(home)
	})
	return h
}

func (h *watchHome) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, mpingBin, args...)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "HOME=") && !strings.HasPrefix(v, "MPING_SERVER=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+h.home)
	return cmd
}

var startedServer = regexp.MustCompile(`started the server \(pid (\d+)\)`)

// run runs mping to its end.
func (h *watchHome) run(args ...string) (stdout, stderr string, exitCode int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := h.command(ctx, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut // pipes: Run returns once nobody has them open
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil && (!errors.As(err, &exitErr) || ctx.Err() != nil) {
		h.t.Errorf("mping %v: %v", args, err)
	}
	if m := startedServer.FindStringSubmatch(errOut.String()); m != nil {
		pid, _ := strconv.Atoi(m[1])
		h.mu.Lock()
		h.pids = append(h.pids, pid)
		h.mu.Unlock()
	}
	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}

func (h *watchHome) ok(args ...string) (stdout, stderr string) {
	h.t.Helper()
	stdout, stderr, code := h.run(args...)
	if code != 0 {
		h.t.Fatalf("mping %v: exit code %d\n%s\n%s", args, code, stdout, stderr)
	}
	return stdout, stderr
}

// fails wants exit code 1 with a message on stderr.
func (h *watchHome) fails(want string, args ...string) {
	h.t.Helper()
	stdout, stderr, code := h.run(args...)
	if code != 1 || stdout != "" || !strings.Contains(stderr, want) {
		h.t.Errorf("mping %v: exit code %d\n%s\n%s\nwant %q", args, code, stdout, stderr, want)
	}
}

// start starts a watch, and wants a server to be started.
func (h *watchHome) start(name, target string) (pid int) {
	h.t.Helper()
	n := len(h.pids)
	stdout, stderr := h.ok("watch", "start", "--name", name, "-i", "200", "-t", "200", target)
	if len(h.pids) != n+1 || !strings.HasPrefix(stdout, "started "+name+" (w-") {
		h.t.Fatalf("%s\n%s", stdout, stderr)
	}
	return h.pids[n]
}

// gone waits for a process to end, for which somebody else waits.
func (h *watchHome) gone(pid int) bool {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		// Where the one that inherits orphans does not wait for them
		if out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output(); strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			return true
		}
	}
	return false
}

func (h *watchHome) list() (names []string) {
	h.t.Helper()
	stdout, _ := h.ok("watch", "ls", "-o", "json")
	var list struct {
		Watches []struct{ Name, State string }
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		h.t.Fatalf("%v\n%s", err, stdout)
	}
	for _, w := range list.Watches {
		names = append(names, w.Name)
	}
	return names
}

func target(t *testing.T) string {
	return "tcp://" + newTCPServer(t, "tcp4", "127.0.0.1:0").String()
}

func TestWatch(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	tcp := target(t)
	if _, stderr := h.ok("watch", "ls"); !strings.Contains(stderr, "not running") {
		t.Error(stderr)
	}
	h.fails("not running", "watch", "show", "a")
	h.fails("no targets", "watch", "start", "--name", "a")
	if _, err := os.Stat(filepath.Join(h.home, ".mping")); err == nil {
		t.Error("something was started")
	}
	pid := h.start("a", tcp)

	// It has nothing of the process that started it
	if sid, err := unix.Getsid(pid); err != nil || sid != pid {
		t.Errorf("session %d of process %d, %v", sid, pid, err)
	}
	if out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output(); err != nil || strings.Trim(string(out), " \n?-") != "" {
		t.Errorf("terminal %q, %v", out, err)
	}
	if names := h.list(); len(names) != 1 || names[0] != "a" {
		t.Error(names)
	}
	h.ok("watch", "start", "--name", "b", tcp)
	if len(h.pids) != 1 {
		t.Error("another server was started")
	}
	h.fails("already running", "server")
	if stdout, _ := h.ok("watch", "stop", "a"); !strings.Contains(stdout, "stopped (requested): 1 target: 0 down, 0 degraded, 0 pending, 1 ok") {
		t.Error(stdout)
	}
	h.ok("watch", "show", "a")
	h.ok("watch", "rm", "a")

	h.fails("use -f to stop them\n  b (w-", "server", "stop")
	if names := h.list(); len(names) != 1 {
		t.Error(names)
	}
	h.ok("server", "stop", "-f")
	if !h.gone(pid) {
		t.Error("the server is still there")
	}
	h.ok("server", "stop")
}

// The end of the terminal that a server was started from is not its end, and
// what the starting process was given is not kept open.
func TestWatchOutlivesTerminal(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	script := fmt.Sprintf(`"$0" watch start --name a -i 200 -t 200 %s; echo done; read -r line`, target(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := h.command(ctx, "-c", script, mpingBin)
	cmd.Path = "/bin/sh"
	cmd.ExtraFiles = []*os.File{w}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe() // the shell waits for its end
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	var out []byte
	for b := make([]byte, 1000); !bytes.Contains(out, []byte("done")); {
		n, err := stdout.Read(b)
		if out = append(out, b[:n]...); err != nil {
			break
		}
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Error(err)
	}
	stdin.Close() // in case SIGHUP is ignored, as below nohup
	cmd.Wait()
	m := startedServer.FindStringSubmatch(stderr.String())
	if m == nil {
		t.Fatalf("%s\n%s", out, stderr.String())
	}
	pid, _ := strconv.Atoi(m[1])
	h.pids = append(h.pids, pid)
	r.SetReadDeadline(time.Now().Add(10 * time.Second))
	if b, err := io.ReadAll(r); err != nil || len(b) > 0 {
		t.Errorf("%q %v", b, err)
	}
	time.Sleep(200 * time.Millisecond)
	if names := h.list(); len(names) != 1 {
		t.Error(names)
	}
}

func TestWatchConcurrentStarts(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	tcp := target(t)
	const n = 10
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			if _, stderr, code := h.run("watch", "start", "--name", fmt.Sprint("w", i), tcp); code != 0 {
				t.Errorf("exit code %d: %s", code, stderr)
			}
		})
	}
	wg.Wait()
	if names := h.list(); len(names) != n {
		t.Error(names)
	}
	if len(h.pids) != 1 {
		t.Errorf("servers %v", h.pids)
	}
}

func TestWatchAfterCrash(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	tcp := target(t)
	pid := h.start("a", tcp)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil || !h.gone(pid) {
		t.Fatal(err)
	}
	if _, stderr := h.ok("watch", "ls"); !strings.Contains(stderr, "not running") {
		t.Error(stderr)
	}
	h.fails("not running", "watch", "show", "a")
	h.start("b", tcp)
	if names := h.list(); len(names) != 1 || names[0] != "b" {
		t.Error(names)
	}
}

func TestWatchBrokenConfig(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	config := filepath.Join(h.home, ".mping.yml")
	if err := os.WriteFile(config, []byte("prober: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	h.fails(`failed to load config "`+config, "watch", "start", "--name", "a", "tcp://127.0.0.1:1")
	h.fails("the server did not start", "watch", "start", "--name", "a", "tcp://127.0.0.1:1")
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	h.start("a", "tcp://127.0.0.1:1")
}

func TestWatchFollow(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	h.start("a", target(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := h.command(ctx, "watch", "events", "-F", "a")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	h.ok("watch", "stop", "a")
	if err := cmd.Wait(); err != nil || strings.Count(out.String(), "\n") != 2 || !strings.HasSuffix(out.String(), "watch_stopped: requested\n") {
		t.Errorf("%v\n%s", err, out.String())
	}
}

// The server in the foreground, until it is idle or interrupted.
func TestServerForeground(t *testing.T) {
	t.Parallel()
	h := newWatchHome(t)
	socket := filepath.Join(h.home, "x", "s")
	if _, stderr := h.ok("server", "--idle-exit", "500ms", "--listen", "unix://"+socket); !strings.Contains(stderr, "shutting down") {
		t.Error(stderr)
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("the socket is still there")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := h.command(ctx, "server")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, stderr, _ := h.run("watch", "ls"); stderr == "no watches\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no server")
		}
	}
	h.ok("watch", "start", "--name", "a", "-t", "100", "tcp://127.0.0.1:1")
	if len(h.pids) > 0 {
		t.Error("another server was started")
	}
	cmd.Process.Signal(syscall.SIGTERM)
	if err := cmd.Wait(); err != nil || !strings.Contains(stderr.String(), "shutting down") {
		t.Errorf("%v\n%s", err, stderr.String())
	}
	h.fails("not running", "watch", "show", "a")
}
