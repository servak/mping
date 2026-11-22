package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/icmpid"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

// params returns the parameters of a fast session on the given targets.
func params(cfg *config.Config, targets ...string) Params {
	return Params{Config: cfg, Targets: targets, Interval: 20 * time.Millisecond, Timeout: time.Second}
}

func mustNew(t *testing.T, p Params) *Session {
	t.Helper()
	s, err := New(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, s) })
	return s
}

// stop fails the test, rather than hang, when s does not stop.
func stop(t *testing.T, s *Session) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Stop()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Stop did not return")
	}
}

// only returns the metrics of the single target of s.
func only(t *testing.T, s *Session) stats.Metrics {
	t.Helper()
	m := s.Metrics().SortBy(stats.Host, true)
	if len(m) != 1 {
		t.Fatalf("session has %d targets, want 1", len(m))
	}
	return m[0]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitTotal waits until every session has sent at least n probes to its
// single target.
func waitTotal(t *testing.T, n int, sessions ...*Session) {
	t.Helper()
	waitFor(t, "probes", func() bool {
		for _, s := range sessions {
			m := s.Metrics().SortBy(stats.Host, true)
			if len(m) != 1 || m[0].GetTotal() < n {
				return false
			}
		}
		return true
	})
}

// tcpServer accepts connections on loopback and returns its address.
func tcpServer(t *testing.T) string {
	return serve(t, func(c net.Conn) { c.Close() })
}

// silentServer accepts connections but never answers.
func silentServer(t *testing.T) string {
	return serve(t, func(c net.Conn) {
		_, _ = io.Copy(io.Discard, c) // until the client closes it
		c.Close()
	})
}

// serve handles TCP connections on loopback and returns its address.
func serve(t *testing.T, handle func(net.Conn)) string {
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
			go handle(c)
		}
	}()
	return ln.Addr().String()
}

// repeats is how often checkLeaks repeats what might leak, so that a leak
// stands out from the goroutines that come and go in the runtime.
const repeats = 10

// checkLeaks runs f repeatedly and fails the test when goroutines or ICMP
// echo IDs are left behind.
func checkLeaks(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	goroutines, ids := runtime.NumGoroutine(), icmpid.Default().Held()
	for range repeats {
		f(t)
	}
	if n := icmpid.Default().Held() - ids; n != 0 {
		t.Errorf("%d ICMP echo IDs still held", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() >= goroutines+repeats/2 {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutine leak: %d running, %d before\n%s", runtime.NumGoroutine(), goroutines, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// needICMP skips the test where no ICMP socket can be opened, or fails it if
// MPING_IT_STRICT is 1 (see internal/prober/icmp_integration_test.go). The
// tests that use it are called TestIntegration..., so that CI runs them on
// both types of socket, although they need no build tag.
func needICMP(t *testing.T) {
	t.Helper()
	p, err := prober.NewICMPProber(prober.ICMPV4, &prober.ICMPConfig{}, "icmpv4")
	if err == nil {
		p.Stop()
		return
	}
	if os.Getenv("MPING_IT_STRICT") == "1" {
		t.Fatal(err)
	}
	t.Skip(err)
}

func TestNewRejectsBadTargets(t *testing.T) {
	for _, tc := range []struct {
		targets []string
		want    string
	}{
		{[]string{"bogus://foo"}, "failed to add targets: failed to route target bogus://foo: unable to determine prober type for target: bogus://foo"},
		{[]string{"tcp://127.0.0.1:1", "bogus://foo", "http://127.0.0.1/", "tcp://noport"}, "failed to add targets: 2 targets failed\n" +
			"  bogus://foo: unable to determine prober type for target: bogus://foo\n" +
			"  tcp://noport: prober tcp rejected target tcp://noport: invalid TCP target: invalid target format: noport"},
	} {
		_, err := New(context.Background(), params(config.DefaultConfig(), tc.targets...))
		if err == nil || err.Error() != tc.want {
			t.Errorf("New(%q) = %v\nwant %s", tc.targets, err, tc.want)
		}
		var errs prober.TargetErrors
		if !errors.As(err, &errs) {
			t.Errorf("New(%q) = %v, want TargetErrors", tc.targets, err)
		}
	}
}

func TestNewRejectsBadParams(t *testing.T) {
	for name, modify := range map[string]func(*Params){
		"no config":  func(p *Params) { p.Config = nil },
		"no targets": func(p *Params) { p.Targets = []string{} },
		"interval must be greater than zero, got 0s":  func(p *Params) { p.Interval = 0 },
		"interval must be greater than zero, got -1s": func(p *Params) { p.Interval = -time.Second },
		"timeout must be greater than zero, got 0s":   func(p *Params) { p.Timeout = 0 },
		"timeout must be greater than zero, got -1ns": func(p *Params) { p.Timeout = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			p := params(config.DefaultConfig(), "tcp://127.0.0.1:1")
			modify(&p)
			s, err := New(context.Background(), p)
			if s != nil || err == nil || err.Error() != "invalid session: "+name {
				t.Errorf("New() = %v, %v", s, err)
			}
		})
	}
}

// Sessions created at the same time from one config, with different source
// interfaces, probe the same target each from its own source, and leave the
// config alone. The one whose source address is not of this host fails.
func TestSessionsDoNotShareConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	target := "tcp://" + tcpServer(t)
	sources := []string{"127.0.0.1", "192.0.2.77", "", "192.0.2.78"}

	sessions := make([]*Session, len(sources))
	errs := make([]error, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Go(func() {
			p := params(cfg, target)
			p.SourceInterface = source
			sessions[i], errs[i] = New(context.Background(), p)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, sessions[i]) })
	}
	if !reflect.DeepEqual(cfg, config.DefaultConfig()) {
		t.Errorf("config was modified:\n%s", config.Marshal(cfg))
	}

	for _, s := range sessions {
		s.Start(context.Background())
	}
	waitTotal(t, 5, sessions...)
	for i, s := range sessions {
		s.Stop()
		m := only(t, s)
		want, got := m.GetTotal(), m.GetSuccessful()
		if strings.HasPrefix(sources[i], "192.0.2.") {
			got = m.GetFailed()
		}
		if got != want {
			t.Errorf("source %q: %d sent, %d successful, %d failed (%s)", sources[i],
				m.GetTotal(), m.GetSuccessful(), m.GetFailed(), m.GetLastFailDetail())
		}
	}
}

// A New that fails releases the sockets, echo IDs and goroutines of the
// targets it did add.
func TestIntegrationFailedNewReleasesAll(t *testing.T) {
	needICMP(t)
	for name, tc := range map[string]struct {
		source  string
		targets []string
	}{
		"unknown prober":            {"", []string{"127.0.0.1", "icmpv4://127.0.0.1", "bogus://127.0.0.1"}},
		"rejected by its prober":    {"", []string{"icmpv4://::1"}},
		"rejected by a later one":   {"", []string{"127.0.0.1", "tcp://noport"}},
		"no socket for the source":  {"192.0.2.77", []string{"http://127.0.0.1/", "127.0.0.1"}},
		"no source for the family":  {"127.0.0.1", []string{"127.0.0.1", "icmpv6://::1"}},
		"no TCP source (interface)": {"mping-no-such-if", []string{"tcp://127.0.0.1:1"}},
	} {
		t.Run(name, func(t *testing.T) {
			checkLeaks(t, func(t *testing.T) {
				p := params(config.DefaultConfig(), tc.targets...)
				p.SourceInterface = tc.source
				if s, err := New(context.Background(), p); err == nil {
					s.Stop()
					t.Fatal("New() did not fail")
				}
			})
		})
	}
}

// dialDNS connects to DNS servers; see TestMain and useDNS.
var dialDNS atomic.Pointer[func(network, address string) (net.Conn, error)]

// The resolver is set once, before any goroutine uses it: lookups may outlive
// the test that caused them.
func TestMain(m *testing.M) {
	dial := net.Dial
	dialDNS.Store(&dial)
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(_ context.Context, network, address string) (net.Conn, error) {
			return (*dialDNS.Load())(network, address)
		},
	}
	os.Exit(m.Run())
}

// useDNS makes dial, which returns no connection, stand in for the connections
// to DNS servers.
func useDNS(t *testing.T, dial func() error) {
	orig := dialDNS.Load()
	f := func(string, string) (net.Conn, error) { return nil, dial() }
	dialDNS.Store(&f)
	t.Cleanup(func() { dialDNS.Store(orig) })
}

// A New that is canceled while the resolver hangs returns at once, and leaves
// nothing behind either.
func TestIntegrationCanceledNewReleasesAll(t *testing.T) {
	needICMP(t)
	targets := []string{"127.0.0.1", "icmpv6://::1", "http://127.0.0.1/", "tcp://127.0.0.1:1"}
	for i := range 100 {
		targets = append(targets, fmt.Sprintf("h%d.test.", i), fmt.Sprintf("tcp://h%d.test.:80", i))
	}
	base := icmpid.Default().Held()
	checkLeaks(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		defer close(release)
		useDNS(t, func() error {
			cancel()
			<-release // whatever the context
			return io.ErrUnexpectedEOF
		})
		done := make(chan error, 1)
		go func() {
			_, err := New(ctx, params(config.DefaultConfig(), targets...))
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("New() = %v, want context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("New did not return")
		}
		if n := icmpid.Default().Held(); n != base {
			t.Errorf("%d ICMP echo IDs held after New, want %d", n, base)
		}
	})
}

// No target makes New or a session panic, however strange it is.
func TestStrangeTargets(t *testing.T) {
	useDNS(t, func() error { return io.ErrUnexpectedEOF })
	cfg := config.DefaultConfig()
	cfg.Prober["dns"].DNS.Server, cfg.Prober["ntp"].NTP.Server = "127.0.0.1", "127.0.0.1"
	long := strings.Repeat("a", 70000)
	targets := []string{"://", ":", "::", "/", "//", " ", "bogus://", "bogus:", "\x00", "\xff", long, "://" + long}
	for _, scheme := range []string{"", "icmpv4://", "icmpv6://", "http://", "https://", "tcp://", "dns://", "ntp://", "icmpv4:", "tcp:", "dns:", "ntp:", "http:"} {
		for _, host := range []string{"", "/", "//", "://", ":", "[", "]", "[]", "[::1", "[::1]", "[::1]x", "::", "%", "::%", "::1%x", "fe80::1%",
			"\x00", "\x1b[2J", "\xff\xfe", "a\nb", " ", "..", long, "@", "?", "#", "127.0.0.1%0"} {
			for _, rest := range []string{"", ":", ":0", ":-1", ":65536", ":1", ":domain", ":" + long, "/", "/x", "/\x00", "/\xff/A", "/" + long, "/a..b", "/\\", "/x/", "/x/bogus", ":1/x/MX"} {
				targets = append(targets, scheme+host+rest)
			}
		}
	}
	_, err := New(context.Background(), params(cfg, targets...))
	var errs prober.TargetErrors
	if !errors.As(err, &errs) {
		t.Fatalf("New() = %v, want TargetErrors", err)
	}
	var noICMP prober.ProberError
	errors.As(err, &noICMP)
	bad := make(map[string]bool)
	for _, e := range errs {
		bad[e.Target] = true
	}
	var accepted []string
	for _, target := range targets {
		scheme, _, _ := strings.Cut(target, ":")
		pc := cfg.Prober[scheme]
		isICMP := pc == nil || pc.ICMP != nil // the default is ICMP
		if !bad[target] && (!isICMP || noICMP.Err == nil) {
			accepted = append(accepted, target)
		}
	}
	t.Logf("%d of %d targets accepted", len(accepted), len(targets))
	if len(accepted) == 0 || len(bad) == 0 {
		t.Fatal("all or nothing accepted")
	}
	p := params(cfg, accepted...)
	p.Interval, p.Timeout = time.Minute, 100*time.Millisecond // one round
	s := mustNew(t, p)
	s.Start(context.Background())
	waitFor(t, "results", func() bool {
		metrics := s.Metrics().SortBy(stats.Host, true)
		return len(metrics) > 0 && !slices.ContainsFunc(metrics, func(m stats.Metrics) bool {
			return m.GetSuccessful()+m.GetFailed() == 0
		})
	})
	stop(t, s)
}

// However a session ends, it leaves nothing behind, and ending it again does
// no harm.
func TestIntegrationStopReleasesAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	tcp, silent := tcpServer(t), silentServer(t)
	cfg := config.DefaultConfig()
	cfg.Prober["http"].HTTP.KeepAlive = true
	targets := []string{srv.URL, "tcp://" + tcp, "http://" + silent, "https://" + silent, "dns://" + silent + "/x.test"}
	base := icmpid.Default().Held()
	stopped := func(t *testing.T, s *Session) {
		t.Helper()
		stop(t, s)
		if n := icmpid.Default().Held(); n != base {
			t.Errorf("%d ICMP echo IDs held after Stop, want %d", n, base)
		}
	}
	// probed waits for the first results
	probed := func(t *testing.T, s *Session) {
		t.Helper()
		waitFor(t, "results", func() bool {
			m := s.Metrics().SortBy(stats.Success, false)
			return len(m) > 0 && m[0].GetSuccessful() > 0
		})
	}
	run := func(t *testing.T, targets []string) {
		for name, end := range map[string]func(t *testing.T, s *Session){
			"stop without start": func(t *testing.T, s *Session) { stopped(t, s) },
			"start after stop": func(t *testing.T, s *Session) {
				stopped(t, s)
				s.Start(context.Background())
			},
			"stop at once": func(t *testing.T, s *Session) {
				s.Start(context.Background())
				stopped(t, s)
			},
			"stop while probing": func(t *testing.T, s *Session) {
				s.Start(context.Background())
				probed(t, s)
				stopped(t, s)
			},
			"concurrent stops": func(t *testing.T, s *Session) {
				s.Start(context.Background())
				probed(t, s)
				var wg sync.WaitGroup
				for range 3 {
					wg.Go(func() { stopped(t, s) })
				}
				wg.Wait()
			},
			"context done": func(t *testing.T, s *Session) {
				ctx, cancel := context.WithCancel(context.Background())
				s.Start(ctx)
				probed(t, s)
				cancel()
				select {
				case <-s.done:
				case <-time.After(5 * time.Second):
					t.Fatal("session did not end with its context")
				}
				if n := icmpid.Default().Held(); n != base {
					t.Errorf("%d ICMP echo IDs held after the context, want %d", n, base)
				}
			},
			"started twice": func(t *testing.T, s *Session) {
				first := s.Start(context.Background())
				probed(t, s)
				select {
				case err := <-s.Start(context.Background()):
					if err == nil {
						t.Error("second Start reported a nil error")
					}
				case <-time.After(5 * time.Second):
					t.Error("second Start was not rejected")
				}
				s.Start(context.Background()) // nobody reads its error
				stopped(t, s)
				select {
				case err := <-first:
					t.Errorf("first Start: %v", err)
				default:
				}
			},
		} {
			t.Run(name, func(t *testing.T) {
				checkLeaks(t, func(t *testing.T) {
					p := params(cfg, targets...)
					p.Timeout = 50 * time.Millisecond
					s := mustNew(t, p)
					end(t, s)
					stop(t, s) // once more
				})
			})
		}
	}
	t.Run("other", func(t *testing.T) { run(t, targets) })
	t.Run("icmp", func(t *testing.T) {
		needICMP(t)
		run(t, []string{"127.0.0.1"})
	})
}

// Results settle for as long as one may still arrive out of order, which the
// interval and timeout tell, and Stop waits until all of them are recorded and
// settled.
func TestStopSettlesMetrics(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // connections are refused
	var recorded atomic.Int32
	p := params(config.DefaultConfig(), "tcp://"+ln.Addr().String())
	p.Timeout = 10 * time.Second // nothing settles by itself during the test
	p.Stats.OnFailure = func(string) {
		time.Sleep(50 * time.Millisecond)
		recorded.Add(1)
	}
	s := mustNew(t, p)
	s.Start(context.Background())
	waitFor(t, "failures", func() bool { return recorded.Load() >= 5 })
	if n := len(only(t, s).GetOutages()); n != 0 {
		t.Errorf("%d outages before the results settled", n)
	}
	stop(t, s)
	before := recorded.Load()
	m := only(t, s)
	if m.GetFailed() != m.GetTotal() || int(before) != m.GetTotal() {
		t.Errorf("after Stop: %d sent, %d failed, %d hooks done", m.GetTotal(), m.GetFailed(), before)
	}
	if n := len(m.GetOutages()); n != 1 {
		t.Errorf("%d outages after Stop, want 1", n)
	}
}

// Sessions that ping the same address at the same time, with the same sequence
// numbers, each get the replies to their own requests, which are told apart by
// their body, and only those.
func TestIntegrationSessionsSameTarget(t *testing.T) {
	needICMP(t)
	for probe, addr := range map[prober.ProbeType]string{prober.ICMPV4: "127.0.0.1", prober.ICMPV6: "::1"} {
		t.Run(string(probe), func(t *testing.T) {
			if probe == prober.ICMPV6 && os.Getenv("MPING_IT_STRICT") != "1" {
				ln, err := net.Listen("tcp", "[::1]:0")
				if err != nil {
					t.Skip(err)
				}
				ln.Close()
			}
			ids := icmpid.Default().Held()
			var sessions []*Session
			for i := range 3 {
				cfg := config.DefaultConfig()
				cfg.Prober[string(probe)].ICMP.Body = fmt.Sprint("session ", i)
				p := params(cfg, string(probe)+"://"+addr)
				p.Interval = 50 * time.Millisecond // macOS answers only some 500 echo requests per second
				sessions = append(sessions, mustNew(t, p))
			}
			if n := icmpid.Default().Held() - ids; n != len(sessions) {
				t.Errorf("%d echo IDs for %d sessions", n, len(sessions))
			}
			for _, s := range sessions {
				s.Start(context.Background())
			}
			waitTotal(t, 15, sessions...)
			for i, s := range sessions {
				stop(t, s)
				m := only(t, s)
				if m.GetSuccessful() != m.GetTotal() {
					t.Errorf("session %d: %d sent, %d successful, %d failed (%s)", i,
						m.GetTotal(), m.GetSuccessful(), m.GetFailed(), m.GetLastFailDetail())
				}
				for _, e := range m.GetRecentHistory(stats.DefaultHistorySize) {
					if e.Details == nil || e.Details.ICMP == nil {
						t.Fatalf("session %d: result without ICMP details: %+v", i, e)
					}
					if got, want := e.Details.ICMP.Payload, fmt.Sprint("session ", i); got != want {
						t.Errorf("session %d got the reply %q", i, got)
					}
				}
			}
		})
	}
}
