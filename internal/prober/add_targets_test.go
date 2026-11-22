package prober

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTargetManager returns a manager that looks names up with r and has one
// prober of every type, the ICMP one (the default) on a fake connection, which
// tells whether it was closed.
func newTargetManager(t *testing.T, r resolver) (*probeManager, *fakeICMPConn) {
	t.Helper()
	pm := NewProbeManager(map[string]*ProberConfig{
		"icmpv4":  {Probe: ICMPV4, ICMP: &ICMPConfig{}},
		"http":    {Probe: HTTP, HTTP: &HTTPConfig{}},
		"tcp":     {Probe: TCP, TCP: &TCPConfig{}},
		"dns":     {Probe: DNS, DNS: &DNSConfig{Server: "192.0.2.53", Port: 53, RecordType: "A"}},
		"ntp":     {Probe: NTP, NTP: &NTPConfig{Server: "192.0.2.123", Port: 123}},
		"invalid": {Probe: "invalid-type"},
	}, "icmpv4").(*probeManager)
	pm.resolver = r
	conn := newFakeICMPConn()
	pm.probers["icmpv4"] = newTestProber(t, ICMPV4, conn, nil, "icmpv4")
	t.Cleanup(pm.Stop)
	return pm, conn
}

// snapshot returns the targets of all probers of pm.
func snapshot(pm *probeManager) map[string]any {
	s := make(map[string]any)
	for name, p := range pm.probers {
		switch p := p.(type) {
		case *ICMPProber:
			s[name] = p.targets
		case *HTTPProber:
			s[name] = p.targets
		case *TCPProber:
			s[name] = p.targets
		case *DNSProber:
			s[name] = p.targets
		case *NTPProber:
			s[name] = p.targets
		}
	}
	return s
}

// addTargets fails the test, rather than hang, when AddTargets does not return.
func addTargets(t *testing.T, pm *probeManager, ctx context.Context, targets ...string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- pm.AddTargets(ctx, targets...) }()
	select {
	case err := <-done:
		return err
	case <-time.After(stopBudget):
		t.Fatal("AddTargets did not return")
		return nil
	}
}

// Whatever the order in which the lookups finish, the targets are those of
// adding them one by one.
func TestAddTargetsOrder(t *testing.T) {
	targets := []string{
		"b.test", "a.test", "icmpv4://a.test", "192.0.2.1", "c.test", // one address: the first name stays
		"tcp://b.test:80", "tcp:b.test:80", "tcp://a.test:80",
		"dns://b.test/x", "dns://a.test/x", "dns:b.test/x", "dns:///x",
		"ntp://b.test", "ntp://b.test:123", "ntp://a.test", "ntp://", // one key: the last display name stays
		"http://b.test/", "http://a.test/", "http://b.test/",
	}
	addrs := addrResolver(map[string][]string{
		"a.test": {"192.0.2.1"}, "b.test": {"192.0.2.1", "192.0.2.2"}, "c.test": {"2001:db8::1", "192.0.2.3"},
	})
	want, _ := newTargetManager(t, addrs)
	for _, target := range targets {
		if err := want.AddTargets(context.Background(), target); err != nil {
			t.Fatal(err)
		}
	}
	if got := want.probers["icmpv4"].(*ICMPProber).targets["192.0.2.1"].name; got != "b.test(192.0.2.1)" {
		t.Errorf("display name = %q, want that of the first target", got)
	}
	if got := want.probers["ntp"].(*NTPProber).targets["b.test:123"].display; got != "ntp://b.test:123" {
		t.Errorf("display name = %q, want that of the last target", got)
	}

	// The lookups of one name all wait until those of the others are done
	for _, last := range []string{"a.test", "b.test", "c.test"} {
		others := 0
		for _, target := range targets {
			if strings.Contains(target, ".test") && !strings.Contains(target, last) && !strings.HasPrefix(target, "http") {
				others++
			}
		}
		var finished atomic.Int32
		pm, _ := newTargetManager(t, &fakeResolver{lookup: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			if host == last {
				for deadline := time.Now().Add(stopBudget); int(finished.Load()) < others; time.Sleep(time.Millisecond) {
					if time.Now().After(deadline) {
						return nil, errors.New("lookups are not concurrent")
					}
				}
				time.Sleep(10 * time.Millisecond) // whatever the others' callers do next
			} else {
				defer finished.Add(1)
			}
			return addrs.lookup(ctx, host)
		}})
		if err := addTargets(t, pm, context.Background(), targets...); err != nil {
			t.Fatal(err)
		}
		if got := snapshot(pm); !reflect.DeepEqual(got, snapshot(want)) {
			t.Errorf("%s resolved last: targets = %v, want %v", last, got, snapshot(want))
		}
	}
}

// Targets that need no lookup cause none.
func TestAddTargetsNoLookup(t *testing.T) {
	pm, _ := newTargetManager(t, noResolver(t))
	if err := addTargets(t, pm, context.Background(), "127.0.0.1", "icmpv4://192.0.2.1", "icmpv4://", "",
		"tcp://127.0.0.1:80", "tcp://[fe80::1%eth0]:80", "dns://[::1]/x", "dns:///x",
		"ntp://127.0.0.1", "ntp://fe80::1%eth0", "http://name.test/"); err != nil {
		t.Fatal(err)
	}
	// As ever, TCP targets lose the zone and the empty host is accepted
	if got := pm.probers["tcp"].(*TCPProber).targets["tcp://[fe80::1%eth0]:80"].addr; got != "[fe80::1]:80" {
		t.Errorf("TCP address = %s", got)
	}
	if _, ok := pm.probers["icmpv4"].(*ICMPProber).targets[""]; !ok {
		t.Error("the empty host is not a target")
	}
}

// Every failing target is reported, in order and once, and nothing is added.
func TestAddTargetsReportsAllFailures(t *testing.T) {
	pm, conn := newTargetManager(t, addrResolver(map[string][]string{"ok.test": {"192.0.2.1"}}))
	if err := pm.AddTargets(context.Background(), "192.0.2.9"); err != nil {
		t.Fatal(err)
	}
	before := fmt.Sprint(snapshot(pm))

	bad := []string{"bogus://x", "none.test", "tcp://noport", "tcp:none.test:80", "dns://none.test/x", "ntp://none.test",
		"http://", "icmpv4:none.test", "://"}
	targets := []string{"ok.test", "tcp://ok.test:80"}
	targets = append(targets, bad...)
	targets = append(targets, "none.test", "http://ok.test/", "ntp://ok.test", "dns://ok.test/x")
	err := fmt.Errorf("wrapped: %w", addTargets(t, pm, context.Background(), targets...))

	var errs TargetErrors
	if !errors.As(err, &errs) {
		t.Fatalf("AddTargets() = %v, want TargetErrors", err)
	}
	var got []string
	for _, e := range errs {
		got = append(got, e.Target)
		if strings.Contains(e.Target, "none.test") != errors.Is(e, errNoSuchHost) {
			t.Errorf("error of %s = %v", e.Target, e.Err)
		}
	}
	if !slices.Equal(got, bad) {
		t.Errorf("failed targets = %q, want %q", got, bad)
	}
	var one TargetError
	if !errors.As(err, &one) || one.Target != bad[0] || !errors.Is(err, errNoSuchHost) {
		t.Errorf("AddTargets() = %v, does not wrap the errors of the targets", err)
	}
	var proberErr ProberError
	if errors.As(err, &proberErr) {
		t.Errorf("AddTargets() = %v", proberErr)
	}

	if after := fmt.Sprint(snapshot(pm)); after != before {
		t.Errorf("targets = %s, want %s", after, before)
	}
	select {
	case <-conn.closed:
		t.Error("the prober of the targets added before was stopped")
	default:
	}
	if err := pm.AddTargets(context.Background(), "ok.test"); err != nil {
		t.Errorf("AddTargets after a failure: %v", err)
	}
}

func TestTargetErrorsMessage(t *testing.T) {
	pm, _ := newTargetManager(t, addrResolver(nil))
	var many []string
	for i := range 13 {
		many = append(many, fmt.Sprintf("x%d://y", i))
	}
	unknown := func(target string) string { return "unable to determine prober type for target: " + target }
	var list strings.Builder
	for _, target := range many[:10] {
		list.WriteString("\n  " + target + ": " + unknown(target))
	}
	for name, tc := range map[string]struct {
		targets []string
		want    string
	}{
		"one unknown prober": {[]string{"192.0.2.1", "x://y"}, "failed to route target x://y: " + unknown("x://y")},
		"one rejected": {[]string{"tcp://noport"},
			"failed to route target tcp://noport: prober tcp rejected target tcp://noport: invalid TCP target: invalid target format: noport"},
		"one unresolvable": {[]string{"none.test"},
			"failed to route target none.test: prober icmpv4 rejected target icmpv4://none.test: failed to resolve 'none.test': no such host (test)"},
		"one given twice": {[]string{"x://y", "x://y"}, "failed to route target x://y: " + unknown("x://y")},
		"two":             {many[:2], "2 targets failed\n  x0://y: " + unknown("x0://y") + "\n  x1://y: " + unknown("x1://y")},
		"ten":             {many[:10], "10 targets failed" + list.String()},
		"many":            {many, "13 targets failed" + list.String() + "\n  and 3 more"},
		"prober":          {[]string{"invalid://a", "invalid://b"}, "failed to get prober for invalid: unknown probe type in config: invalid-type"},
		"prober and target": {[]string{"x://y", "invalid://a", "invalid://b"},
			"failed to get prober for invalid: unknown probe type in config: invalid-type\nfailed to route target x://y: " + unknown("x://y")},
	} {
		t.Run(name, func(t *testing.T) {
			err := addTargets(t, pm, context.Background(), tc.targets...)
			if err == nil || err.Error() != tc.want {
				t.Errorf("AddTargets() = %v\nwant %s", err, tc.want)
			}
		})
	}
}

// A prober that cannot be created is reported once, and not as an error of its
// targets.
func TestAddTargetsProberError(t *testing.T) {
	pm, _ := newTargetManager(t, addrResolver(nil))
	err := addTargets(t, pm, context.Background(), "invalid://a", "x://y", "invalid://b")
	var pe ProberError
	if !errors.As(err, &pe) || pe.Prober != "invalid" {
		t.Errorf("AddTargets() = %v, want a ProberError", err)
	}
	var errs TargetErrors
	if !errors.As(err, &errs) || len(errs) != 1 || errs[0].Target != "x://y" {
		t.Errorf("AddTargets() = %v, want one TargetError", err)
	}
	errs = nil
	if err := addTargets(t, pm, context.Background(), "invalid://a"); errors.As(err, &errs) {
		t.Errorf("AddTargets() = %v, want no TargetErrors", err)
	}
}

// hangingResolver hangs, ignoring the context, until the test ends.
func hangingResolver(t *testing.T) (r *fakeResolver, hanging <-chan struct{}) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	ch := make(chan struct{}, 1000)
	return &fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		ch <- struct{}{}
		<-release
		return nil, errNoSuchHost
	}}, ch
}

// AddTargets returns the error of its context at once, even if lookups hang.
func TestAddTargetsCanceled(t *testing.T) {
	targets := []string{"a.test", "tcp://a.test:80", "dns://a.test/x", "ntp://a.test", "http://a.test/", "x://y"}
	t.Run("canceled", func(t *testing.T) {
		r, hanging := hangingResolver(t)
		pm, _ := newTargetManager(t, r)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-hanging
			cancel()
		}()
		if err := addTargets(t, pm, ctx, targets...); !errors.Is(err, context.Canceled) {
			t.Errorf("AddTargets() = %v, want context.Canceled", err)
		}
		if len(pm.probers) != 1 { // only that of newTargetManager
			t.Errorf("probers = %v", pm.probers)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		r, _ := hangingResolver(t)
		pm, _ := newTargetManager(t, r)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := addTargets(t, pm, ctx, targets...); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("AddTargets() = %v, want context.DeadlineExceeded", err)
		}
	})
	t.Run("before", func(t *testing.T) {
		pm, _ := newTargetManager(t, noResolver(t))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := addTargets(t, pm, ctx, targets...); !errors.Is(err, context.Canceled) {
			t.Errorf("AddTargets() = %v, want context.Canceled", err)
		}
	})
}

// Stop does not wait for lookups, and no target is added after it.
func TestStopDuringAddTargets(t *testing.T) {
	r, hanging := hangingResolver(t)
	pm, conn := newTargetManager(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pm.AddTargets(ctx, "tcp://a.test:80") }()
	<-hanging
	stopped := make(chan struct{})
	go func() {
		pm.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(stopBudget):
		t.Fatal("Stop waits for AddTargets")
	}
	select {
	case <-conn.closed:
	default:
		t.Error("prober not stopped")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("AddTargets() = %v", err)
	}
}

// Targets that were resolved too late are not added.
func TestAddTargetsTooLate(t *testing.T) {
	for name, end := range map[string]func(*probeManager, context.Context){
		"already running": func(pm *probeManager, ctx context.Context) {
			go func() { _ = pm.Run(ctx, time.Hour, time.Second) }()
			for running := false; !running; time.Sleep(time.Millisecond) {
				pm.mu.Lock()
				running = pm.started
				pm.mu.Unlock()
			}
		},
		"stopped": func(pm *probeManager, _ context.Context) { pm.Stop() },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var pm *probeManager
			pm, _ = newTargetManager(t, &fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
				end(pm, ctx)
				return []net.IPAddr{{IP: net.IPv4(192, 0, 2, 1)}}, nil
			}})
			err := addTargets(t, pm, context.Background(), "tcp://a.test:80")
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("AddTargets() = %v", err)
			}
			if pm.probers["tcp"] != nil {
				t.Error("target was added")
			}
		})
	}
}

// Names are looked up concurrently, but not all at once.
func TestAddTargetsBoundsLookups(t *testing.T) {
	var inFlight, most atomic.Int32
	full := make(chan struct{})
	closeFull := sync.OnceFunc(func() { close(full) })
	pm, _ := newTargetManager(t, &fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := most.Load(); n > old && !most.CompareAndSwap(old, n); old = most.Load() {
		}
		if n == maxLookups {
			time.Sleep(20 * time.Millisecond) // time for more
			closeFull()
		}
		select {
		case <-full:
			return []net.IPAddr{{IP: net.IPv4(192, 0, 2, 1)}}, nil
		case <-time.After(stopBudget):
			return nil, errors.New("lookups are not concurrent")
		}
	}})
	var targets []string
	for i := range 3 * maxLookups {
		targets = append(targets, fmt.Sprintf("tcp://h%d.test:80", i))
	}
	if err := pm.AddTargets(context.Background(), targets...); err != nil {
		t.Fatal(err)
	}
	if n := most.Load(); n != maxLookups {
		t.Errorf("%d concurrent lookups, want %d", n, maxLookups)
	}
}
