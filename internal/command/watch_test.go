//go:build darwin || linux || freebsd

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/servak/mping/internal/client"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/server"
)

// TestMain keeps a server that was started by mistake, which would be this
// program, from running the tests, and all from the files of the user.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "server" {
		os.Exit(1)
	}
	os.Setenv("HOME", "/dev/null/home")
	os.Exit(m.Run())
}

// newHome sets HOME, where nothing of the user must be touched, to a new
// directory with a short name and returns it.
func newHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	t.Setenv("MPING_SERVER", "")
	return dir
}

// startTestServer serves on a socket until the end of the test.
func startTestServer(t *testing.T, socket string) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{
			Socket: socket, Version: "v1", Logger: slog.New(slog.DiscardHandler),
			LoadConfig: func() (*config.Config, error) { return config.DefaultConfig(), nil },
		})
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	eventually(t, "the server", func() bool { return listens(socket) })
}

func listens(socket string) bool {
	c, err := net.Dial("unix", socket)
	if err == nil {
		c.Close()
	}
	return err == nil
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// cli runs "mping watch" or "mping server" as version v1.
func cli(args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	cmd := newWatchCmd("v1", &errOut)
	if args[0] == "server" {
		cmd = newServerCmd("v1", &errOut, osSignals)
	}
	cmd.SetArgs(args[1:])
	cmd.SetOut(&out)
	cmd.SetErr(&out) // as main does
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func wantOut(t *testing.T, contains string, args ...string) string {
	t.Helper()
	out, errOut, err := cli(args...)
	if err != nil || errOut != "" || !strings.Contains(out, contains) {
		t.Errorf("%v: %v\n%s\n%s\nwant %q", args, err, out, errOut, contains)
	}
	return out
}

// wantErr wants an error, and nothing else.
func wantErr(t *testing.T, contains string, args ...string) {
	t.Helper()
	out, errOut, err := cli(args...)
	if err == nil || out+errOut != "" || !strings.Contains(err.Error(), contains) {
		t.Errorf("%v: %v\n%s\n%s\nwant %q", args, err, out, errOut, contains)
	}
}

func wantJSON[T any](t *testing.T, args ...string) (v T) {
	t.Helper()
	out, errOut, err := cli(append(args, "-o", "json")...)
	if err == nil {
		err = json.Unmarshal([]byte(out), &v)
	}
	if err != nil || errOut != "" {
		t.Fatalf("%v: %v\n%s\n%s", args, err, out, errOut)
	}
	return v
}

func tcpTarget(t *testing.T) (string, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
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
	return "tcp://" + ln.Addr().String(), ln
}

func TestWatchCommands(t *testing.T) {
	home := newHome(t)
	startTestServer(t, filepath.Join(home, ".mping", "server.sock"))
	up, _ := tcpTarget(t)
	down, ln := tcpTarget(t)
	ln.Close()
	file := filepath.Join(home, "targets")
	if err := os.WriteFile(file, []byte("# targets\n"+down+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if out, errOut, err := cli("watch", "ls"); out != "" || errOut != "no watches\n" || err != nil {
		t.Error(out, errOut, err)
	}
	wantOut(t, "2 targets, ends in 1h30m (", "watch", "start", "--name", "demo", "--for", "90m", "-i", "200", "-t", "100", "-f", file, up)
	w := wantJSON[api.WatchDetail](t, "watch", "start", "--name", "other", "-I", "127.0.0.1", up)
	if w.Name != "other" || w.IntervalMs != 1000 || w.TimeoutMs != 1000 || w.SourceInterface != "127.0.0.1" || w.ExpiresAt.Sub(w.StartedAt) != 2*time.Hour {
		t.Errorf("%+v", w)
	}
	list := wantJSON[api.WatchList](t, "watch", "ls")
	if len(list.Watches) != 2 || list.Watches[0].Name != "demo" || list.Watches[0].IntervalMs != 200 || list.Watches[0].TimeoutMs != 100 {
		t.Errorf("%+v", list)
	}
	id := list.Watches[0].ID
	if out := wantOut(t, "NAME", "watch", "ls"); strings.Index(out, "demo") > strings.Index(out, "other") {
		t.Errorf("not the newest last:\n%s", out)
	}
	eventually(t, "results", func() bool {
		c := wantJSON[api.Summary](t, "watch", "show", "demo").Counts
		return c.Down == 1 && c.OK == 1 && strings.Contains(wantOut(t, "", "watch", "events", "demo"), "outage_start")
	})
	if out := wantOut(t, down, "watch", "show", "demo"); strings.Contains(out, up) {
		t.Errorf("with a target that is ok:\n%s", out)
	}
	wantOut(t, up, "watch", "show", id, "-a")
	if sum := wantJSON[api.Summary](t, "watch", "show", "demo", "--all"); len(sum.Targets) != 2 || sum.Targets[0].Host != down {
		t.Errorf("%+v", sum)
	}
	if out := wantOut(t, "#1 watch_started: until", "watch", "events", "demo"); strings.Count(out, "\n") != 2 {
		t.Errorf("%s", out)
	}
	if e := wantJSON[api.Event](t, "watch", "events", id, "--after", "1"); e.Kind != "outage_start" || e.Seq != 2 || e.Target != down {
		t.Errorf("%+v", e)
	}
	wantOut(t, "", "watch", "events", "demo", "--after", "99")

	wantOut(t, "demo ("+id+") ends in 36h00m (", "watch", "extend", "demo", "--for", "1d12h")
	wantErr(t, "earlier", "watch", "extend", "demo", "--for", "1h")
	wantErr(t, "has not stopped yet", "watch", "start", "--name", "demo", up)
	wantErr(t, "is running; use -f", "watch", "rm", "demo")
	wantErr(t, "\n  nope://x: unable", "watch", "start", "--name", "bad", up, "nope://x", "tcp://noport")
	wantErr(t, "\n  tcp://noport: ", "watch", "start", "--name", "bad", up, "nope://x", "tcp://noport")
	for _, verb := range []string{"show", "events", "stop", "rm"} {
		wantErr(t, `no watch "nope"`, "watch", verb, "nope")
	}
	_, _, err := cli("server", "stop")
	if err == nil || !strings.Contains(err.Error(), "use -f") ||
		!strings.Contains(err.Error(), "\n  demo ("+id+"), running, ends in 36h00m\n  other (") {
		t.Error(err)
	}

	wantOut(t, "demo ("+id+") stopped (requested): 2 targets: 1 down, 0 degraded, 0 pending, 1 ok\n", "watch", "stop", "demo")
	wantOut(t, "stopped (requested)", "watch", "stop", "demo")
	if sum := wantJSON[api.Summary](t, "watch", "show", "demo"); sum.State != api.StateStopped {
		t.Errorf("%+v", sum)
	}
	// Following ends with the watch, also beyond its events
	wantOut(t, "watch_stopped: requested\n", "watch", "events", "-F", "demo")
	wantOut(t, "", "watch", "events", "-F", "demo", "--after", "9999")
	wantOut(t, "removed demo ("+id+")", "watch", "rm", "demo")
	wantErr(t, "no watch", "watch", "show", id)
	wantOut(t, "removed other", "watch", "rm", "-f", "other")
	wantOut(t, "the server has ended", "server", "stop", "-f")

	// It is what stop was to achieve; nothing to list is no error, all else is
	out, errOut, err := cli("server", "stop")
	if out != "" || !strings.Contains(errOut, "not running") || err != nil {
		t.Error(out, errOut, err)
	}
	if out, errOut, err := cli("watch", "ls"); out != "" || !strings.Contains(errOut, "not running") || err != nil {
		t.Error(out, errOut, err)
	}
	if out, errOut, err := cli("watch", "ls", "-o", "json"); out != "{\n  \"watches\": []\n}\n" || !strings.Contains(errOut, "not running") || err != nil {
		t.Error(out, errOut, err)
	}
	for _, args := range []string{"show x", "events x", "events -F x", "extend --for 1h x", "stop x", "rm x"} {
		wantErr(t, `not running; "mping watch start" starts it`, append([]string{"watch"}, strings.Fields(args)...)...)
	}
}

// None of these reaches a server or starts one.
func TestWatchUsageErrors(t *testing.T) {
	home := newHome(t)
	for _, tc := range []struct{ want, args string }{
		{"unknown command", "nope"},
		{"unknown command", "ls x"},
		{"accepts 1 arg", "show"},
		{"accepts 1 arg", "stop a b"},
		{"required flag", "start tcp://127.0.0.1:1"},
		{"required flag", "extend x"},
		{"no targets", "start --name x"},
		{"the name is empty", "start --name= tcp://127.0.0.1:1"},
		{"failed to open", "start --name x -f /nonexistent"},
		{"invalid duration", "start --name x --for 5 tcp://127.0.0.1:1"},
		{"invalid duration", "start --name x --for= tcp://127.0.0.1:1"},
		{"invalid duration", "extend x --for -1h"},
		{"interval must be", "start --name x -i 0 tcp://127.0.0.1:1"},
		{"timeout must be", "start --name x -t -1 tcp://127.0.0.1:1"},
		{"unsupported output", "ls -o csv"},
		{"unsupported output", "events x -o yaml"},
		{"unknown shorthand", "stop x -o json"},
		{"must not be negative", "events x --after -1"},
		{"invalid server address", "ls -s tcp://127.0.0.1:1"},
		{"invalid server address", "ls -s="},
		{"invalid server address", "start --name x -s unix://x tcp://127.0.0.1:1"},
	} {
		wantErr(t, tc.want, append([]string{"watch"}, strings.Fields(tc.args)...)...)
	}
	wantErr(t, "the name is empty", "watch", "rm", "-f", "")
	for _, args := range []string{"nope", "stop x", "--listen tcp://:1", "--listen=", "--listen /a", "-s unix:///a", "stop -s x"} {
		wantErr(t, "", append([]string{"server"}, strings.Fields(args)...)...)
	}
	t.Setenv("MPING_SERVER", "tcp://127.0.0.1:1")
	wantErr(t, "invalid server address", "watch", "ls")
	if entries, err := os.ReadDir(home); err != nil || len(entries) > 0 {
		t.Error(entries, err)
	}
	t.Setenv("MPING_SERVER", "")
	for _, home := range []string{"", "relative"} {
		t.Setenv("HOME", home)
		wantErr(t, "", "watch", "ls")
		wantErr(t, "", "watch", "start", "--name", "x", "tcp://127.0.0.1:1")
		wantErr(t, "", "server", "stop")
	}
	// It is not needed for another socket
	if _, _, err := cli("watch", "show", "x", "-s", "unix:///nonexistent/s"); !errors.Is(err, client.ErrNotRunning) {
		t.Error(err)
	}
}

// A server that was chosen is named, and never started.
func TestWatchServerAddress(t *testing.T) {
	home := newHome(t)
	socket := filepath.Join(home, "s")
	address := "unix://" + socket
	named := "server: " + address + "\n"
	t.Setenv("MPING_SERVER", address)
	out, errOut, err := cli("watch", "start", "--name", "x", "tcp://127.0.0.1:1")
	if out != named || errOut != "" || err == nil || err.Error() != "the server is not running: nothing listens on "+socket {
		t.Error(out, errOut, err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) > 0 {
		t.Error(entries, err)
	}
	startTestServer(t, socket)
	wantOut(t, named+"started x", "watch", "start", "--name", "x", "tcp://127.0.0.1:1")
	out, errOut, err = cli("watch", "ls", "-o", "json")
	if !strings.HasPrefix(out, "{") || errOut != named || err != nil {
		t.Error(out, errOut, err)
	}
	// The flag precedes the environment
	out, errOut, err = cli("watch", "ls", "-s", "unix:///nonexistent/s")
	if out != "server: unix:///nonexistent/s\n" || !strings.Contains(errOut, "not running") || err != nil {
		t.Error(out, errOut, err)
	}
	t.Setenv("MPING_SERVER", "")
	wantOut(t, named+"the server has ended", "server", "stop", "-f", "-s", address)

	// It is not named if it is the usual one, but not started either
	address = "unix://" + filepath.Join(home, ".mping", "server.sock")
	wantErr(t, "nothing listens on", "watch", "start", "--name", "x", "-s", address, "tcp://127.0.0.1:1")
}

func TestWatchVersion(t *testing.T) {
	for _, tc := range []struct {
		server string
		warns  bool
	}{{"v1", false}, {"", false}, {"v2", true}} {
		serveHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(api.VersionHeader, tc.server)
			fmt.Fprint(w, `{"watches":[]}`)
		})
		_, errOut, err := cli("watch", "ls", "-o", "json")
		warns := strings.Contains(errOut, "warning: the server is version v2, this is v1; \"mping server stop\" ends")
		if err != nil || warns != tc.warns || strings.Count(errOut, "warning") > 1 {
			t.Errorf("%+v: %s %v", tc, errOut, err)
		}
	}
	var out bytes.Buffer
	(&remote{Client: &client.Client{ServerVersion: "v2"}}).warnVersion(&out, "") // a development build
	if out.Len() > 0 {
		t.Error(out.String())
	}
}

// serveHTTP serves h on the socket of MPING_SERVER.
func serveHTTP(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	socket := filepath.Join(newHome(t), "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	t.Setenv("MPING_SERVER", "unix://"+socket)
}

// All pages are read, by the ID of the watch.
func TestWatchEventPages(t *testing.T) {
	var requests []string
	serveHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		w.Header().Set(api.VersionHeader, "v2")
		switch r.URL.Query().Get("after") {
		case "":
			fmt.Fprint(w, `{"id":"w-1","name":"x","state":"running"}`)
		case "3":
			fmt.Fprint(w, `{"events":[{"seq":6,"kind":"a"},{"seq":7,"kind":"b"}],"next":7,"more":true,"truncated":true}`)
		case "7":
			fmt.Fprint(w, `{"events":[{"seq":8,"kind":"c"}],"next":8}`)
		}
	})
	out, errOut, err := cli("watch", "events", "x", "--after", "3", "-o", "json")
	var kinds string
	for line := range strings.Lines(out) {
		var e api.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Error(err)
		}
		kinds += e.Kind
	}
	if err != nil || kinds != "abc" || !strings.HasPrefix(errOut, "server: ") || strings.Count(errOut, "\n") != 3 ||
		!strings.Contains(errOut, "\nwarning: the server is version v2") || // at once, not at the end
		!strings.HasSuffix(errOut, "warning: events after #3 have been dropped, the next one is #6\n") {
		t.Error(out, errOut, err)
	}
	if got := strings.Join(requests, " "); got != "/v1/watches/x /v1/watches/w-1/events?after=3 /v1/watches/w-1/events?after=7" {
		t.Error(got)
	}
}

func TestWatchFollow(t *testing.T) {
	home := newHome(t)
	startTestServer(t, filepath.Join(home, ".mping", "server.sock"))
	wantOut(t, "", "watch", "start", "--name", "x", "-i", "200", "-t", "100", "tcp://127.0.0.1:1")
	type result struct {
		out, errOut string
		err         error
	}
	follow := func() chan result {
		done := make(chan result, 1)
		go func() {
			out, errOut, err := cli("watch", "events", "-F", "x", "-o", "json")
			done <- result{out, errOut, err}
		}()
		return done
	}
	done := follow()
	time.Sleep(300 * time.Millisecond)
	wantOut(t, "", "watch", "stop", "x")
	res := <-done
	if res.err != nil || res.errOut != "" || !strings.HasPrefix(res.out, `{"seq":1,"kind":"watch_started"`) ||
		strings.Count(res.out, "watch_started") != 1 || !strings.HasSuffix(res.out, `"stopped":{"reason":"requested"}}`+"\n") {
		t.Errorf("%+v", res)
	}

	wantOut(t, "", "watch", "start", "--name", "x", "-i", "200", "-t", "100", "tcp://127.0.0.1:1")
	done = follow()
	time.Sleep(300 * time.Millisecond)
	wantOut(t, "", "server", "stop", "-f")
	if res := <-done; res.err == nil || !strings.Contains(res.err.Error(), "not running") {
		t.Errorf("%+v", res)
	}
}

// A watch that does not stop is not waited for forever.
func TestWatchStopTimeout(t *testing.T) {
	defer func(d time.Duration) { stopGrace = d }(stopGrace)
	stopGrace = 200 * time.Millisecond
	var polls atomic.Int32
	serveHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		polls.Add(1)
		fmt.Fprint(w, `{"id":"w-1","name":"x","state":"stopping","timeout_ms":100}`)
	})
	start := time.Now()
	_, _, err := cli("watch", "stop", "x")
	if d := time.Since(start); err == nil || err.Error() != "x (w-1) has not stopped yet" || d < 300*time.Millisecond || d > 5*time.Second {
		t.Error(d, err)
	}
	if polls.Load() < 3 {
		t.Error(polls.Load())
	}
}

// runServer runs "mping server" with args and returns what ends it: signals,
// and what tells that it has.
func runServer(t *testing.T, args ...string) (sigs chan os.Signal, unregistered *atomic.Int32, done chan error) {
	t.Helper()
	sigs, unregistered, done = make(chan os.Signal), new(atomic.Int32), make(chan error, 1)
	cmd := newServerCmd("v1", new(bytes.Buffer), func() (<-chan os.Signal, func()) {
		return sigs, func() { unregistered.Add(1) }
	})
	cmd.SetArgs(args)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		done <- cmd.ExecuteContext(ctx)
		close(ended)
	}()
	t.Cleanup(func() {
		cancel()
		<-ended
	})
	return sigs, unregistered, done
}

func TestServerCommand(t *testing.T) {
	home := newHome(t)
	socket := filepath.Join(home, ".mping", "server.sock")
	sigs, unregistered, done := runServer(t)
	eventually(t, "the server", func() bool { return listens(socket) })
	wantErr(t, "already running", "server")

	// The config is read for each watch
	path := filepath.Join(home, ".mping.yml")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantErr(t, "check://127.0.0.1:1", "watch", "start", "--name", "x", "check://127.0.0.1:1")
	write("prober:\n  check:\n    probe: tcp\n    tcp: {}\n")
	wantOut(t, "started x", "watch", "start", "--name", "x", "-t", "100", "check://127.0.0.1:1")
	write("prober: [")
	wantErr(t, path, "watch", "start", "--name", "y", "tcp://127.0.0.1:1")

	sigs <- syscall.SIGTERM
	if err := <-done; err != nil {
		t.Error(err)
	}
	if n := unregistered.Load(); n != 2 { // at the signal and at the end
		t.Errorf("unregistered %d times: a second signal would not kill", n)
	}
	if listens(socket) {
		t.Error("still listens")
	}
	wantErr(t, path, "server")
	wantErr(t, "/nonexistent.yml", "server", "--config", "/nonexistent.yml")

	// A path that is given, also as that of the default one, must exist
	os.Remove(path)
	wantErr(t, path, "server", "--config", path)
	t.Chdir(home)
	write("{}")
	other := filepath.Join(home, "d", "s")
	_, _, done = runServer(t, "--config", ".mping.yml", "--listen", "unix://"+other, "--idle-exit", "300ms")
	eventually(t, "the server", func() bool { return listens(other) })
	if listens(socket) {
		t.Error("listens on the default one")
	}
	t.Chdir("/")
	wantOut(t, "started x", "watch", "-s", "unix://"+other, "start", "--name", "x", "-t", "100", "tcp://127.0.0.1:1")
	wantOut(t, "", "watch", "-s", "unix://"+other, "rm", "-f", "x")
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Error("no idle exit")
	}
	if flag := NewServerCmd("").Flag("idle-exit"); !flag.Hidden {
		t.Error("--idle-exit is not hidden")
	}
}

func TestExplain(t *testing.T) {
	r := &remote{given: true}
	if err := errors.New("x"); r.explain(err) != err || r.explain(nil) != nil {
		t.Error("changed")
	}
	e := api.Errorf(api.CodeUnresolvable, "2 of 3")
	e.Targets = []api.TargetError{{Target: "a\x1b", Message: "b\nc"}, {Target: "d", Message: "e"}}
	err := r.explain(e)
	if err.Error() != "2 of 3:\n  a: b c\n  d: e" || !errors.Is(err, e) {
		t.Errorf("%q", err)
	}
}
