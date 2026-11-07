package prober

// Start/Stop lifecycle tests: every prober is started against a server on
// loopback, must report REGISTER, SENT and SUCCESS for each target, and must
// return from Start promptly once Stop is called without leaking goroutines.
// ICMP needs a raw socket and is covered by icmp_integration_test.go.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	lifecycleInterval = 50 * time.Millisecond
	lifecycleTimeout  = time.Second
	// stopBudget bounds how long Stop and the return of Start may take.
	stopBudget = 3 * time.Second
)

// eventLog collects the events a prober emits.
type eventLog struct {
	mu     sync.Mutex
	events []*Event
	done   chan struct{}
}

func collectEvents(ch <-chan *Event) *eventLog {
	l := &eventLog{done: make(chan struct{})}
	go func() {
		defer close(l.done)
		for e := range ch {
			l.mu.Lock()
			l.events = append(l.events, e)
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *eventLog) snapshot() []*Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Event(nil), l.events...)
}

func (l *eventLog) count(r reason) int {
	n := 0
	for _, e := range l.snapshot() {
		if e.Result == r {
			n++
		}
	}
	return n
}

// seen reports whether every key has at least one event with each result.
func (l *eventLog) seen(keys []string, results ...reason) bool {
	got := map[string]map[reason]bool{}
	for _, e := range l.snapshot() {
		if got[e.Key] == nil {
			got[e.Key] = map[reason]bool{}
		}
		got[e.Key][e.Result] = true
	}
	for _, k := range keys {
		for _, r := range results {
			if !got[k][r] {
				return false
			}
		}
	}
	return true
}

// waitGoroutines fails the test when the goroutine count does not drop back
// to base (plus a small tolerance for runtime helpers) within a few seconds.
func waitGoroutines(t *testing.T, base int) {
	t.Helper()
	const tolerance = 2
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base+tolerance {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Errorf("goroutine leak: %d running, baseline %d\n%s", runtime.NumGoroutine(), base, buf[:n])
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// goroutineRunning reports whether any goroutine has fn in its stack.
func goroutineRunning(fn string) bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), fn)
}

// lifecycleResult is what runLifecycle observed.
type lifecycleResult struct {
	events []*Event
}

// runLifecycle starts p, waits for REGISTER, SENT and SUCCESS events for all
// keys, stops it and checks that both Stop and Start return within budget.
func runLifecycle(t *testing.T, p Prober, keys []string, interval, timeout time.Duration) lifecycleResult {
	t.Helper()
	events := make(chan *Event, 1000)
	log := collectEvents(events)

	startErr := make(chan error, 1)
	go func() { startErr <- p.Start(events, interval, timeout) }()

	deadline := time.Now().Add(5 * time.Second)
	// Wait for the immediate round and at least one tick.
	for !log.seen(keys, REGISTER, SENT, SUCCESS) || log.count(SENT) < 2*len(keys) {
		if time.Now().After(deadline) {
			t.Fatalf("did not see REGISTER/SENT/SUCCESS for %v; events: %s", keys, describeEvents(log.snapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped := make(chan struct{})
	go func() {
		p.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(stopBudget):
		t.Fatalf("Stop did not return within %v", stopBudget)
	}
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() error = %v", err)
		}
	case <-time.After(stopBudget):
		t.Fatalf("Start did not return within %v of Stop", stopBudget)
	}

	// Start has returned: the prober must not emit anything any more, so
	// closing the channel here also catches late sends (they would panic).
	close(events)
	<-log.done

	got := log.snapshot()
	checkEventOrder(t, got)
	return lifecycleResult{events: got}
}

// checkEventOrder verifies REGISTER comes first for every key and that the
// probes against loopback servers never failed.
func checkEventOrder(t *testing.T, events []*Event) {
	t.Helper()
	registered := map[string]bool{}
	for _, e := range events {
		switch e.Result {
		case REGISTER:
			registered[e.Key] = true
		case SENT:
			if !registered[e.Key] {
				t.Errorf("SENT for %s before REGISTER", e.Key)
			}
		case SUCCESS:
			if e.Rtt <= 0 {
				t.Errorf("SUCCESS for %s with rtt %v", e.Key, e.Rtt)
			}
		default:
			t.Errorf("unexpected result %d for %s: %s", e.Result, e.Key, e.Message)
		}
	}
}

func describeEvents(events []*Event) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "\n  %s result=%d msg=%q", e.Key, e.Result, e.Message)
	}
	return b.String()
}

// stopReturns calls Stop and fails if it panics or hangs.
func stopReturns(t *testing.T, p Prober) {
	t.Helper()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		p.Stop()
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Errorf("Stop panicked: %v", r)
		}
	case <-time.After(stopBudget):
		t.Errorf("Stop did not return within %v", stopBudget)
	}
}

// --- loopback servers ---

func startTCPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	})
	return ln.Addr().String()
}

func startDNSServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	srv := &dns.Server{
		PacketConn:        pc,
		NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
			m := new(dns.Msg)
			m.SetReply(req)
			q := req.Question[0]
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.IPv4(127, 0, 0, 1),
			})
			_ = w.WriteMsg(m)
		}),
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.ActivateAndServe()
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS server did not start")
	}
	t.Cleanup(func() {
		_ = srv.Shutdown()
		<-served
	})
	return pc.LocalAddr().String()
}

func startNTPServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		pc.Close()
		wg.Wait()
	})
	wg.Go(func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			if n < 48 {
				continue
			}
			resp := make([]byte, 48)
			resp[0] = 0x1C // LI=0, VN=3, mode=4 (server)
			resp[1] = 2    // stratum
			sec, frac := ntpTimeFromTime(time.Now())
			binary.BigEndian.PutUint32(resp[40:44], sec)
			binary.BigEndian.PutUint32(resp[44:48], frac)
			_, _ = pc.WriteTo(resp, addr)
		}
	})
	return pc.LocalAddr().String()
}

func startHTTPServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// --- tests ---

type lifecycleCase struct {
	name string
	// setup starts the servers and returns the prober plus the event keys
	// it is expected to report.
	setup func(t *testing.T) (Prober, []string)
}

func lifecycleCases() []lifecycleCase {
	return []lifecycleCase{
		{"tcp", func(t *testing.T) (Prober, []string) {
			addr := startTCPServer(t)
			p := NewTCPProber(&TCPConfig{}, "tcp")
			if err := p.Accept("tcp://" + addr); err != nil {
				t.Fatal(err)
			}
			return p, []string{addr}
		}},
		{"dns", func(t *testing.T) (Prober, []string) {
			addr := startDNSServer(t)
			p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}, "dns")
			target := "dns://" + addr + "/lifecycle.test"
			if err := p.Accept(target); err != nil {
				t.Fatal(err)
			}
			return p, []string{target}
		}},
		{"ntp", func(t *testing.T) (Prober, []string) {
			addr := startNTPServer(t)
			p := NewNTPProber(&NTPConfig{Port: 123}, "ntp")
			if err := p.Accept("ntp://" + addr); err != nil {
				t.Fatal(err)
			}
			return p, []string{addr}
		}},
		{"http", func(t *testing.T) (Prober, []string) {
			addr := startHTTPServer(t)
			p := NewHTTPProber(HTTP, &HTTPConfig{ExpectCodes: "200"}, "http")
			target := "http://" + addr + "/"
			if err := p.Accept(target); err != nil {
				t.Fatal(err)
			}
			return p, []string{target}
		}},
	}
}

func TestProberLifecycle(t *testing.T) {
	for _, tc := range lifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Registered first so it runs after the servers are shut down.
			base := runtime.NumGoroutine()
			t.Cleanup(func() { waitGoroutines(t, base) })

			p, keys := tc.setup(t)
			runLifecycle(t, p, keys, lifecycleInterval, lifecycleTimeout)
			stopReturns(t, p) // second Stop
		})
	}
}

// Stop before Start must not hang, and the later Start must return at once
// without emitting anything.
func TestProberStopBeforeStart(t *testing.T) {
	for _, tc := range lifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := tc.setup(t)
			stopReturns(t, p)
			events := make(chan *Event, 100)
			done := make(chan error, 1)
			go func() { done <- p.Start(events, lifecycleInterval, lifecycleTimeout) }()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Start() after Stop error = %v", err)
				}
			case <-time.After(stopBudget):
				t.Fatal("Start after Stop did not return")
			}
			if n := len(events); n != 0 {
				t.Errorf("Start after Stop emitted %d events", n)
			}
			stopReturns(t, p)
		})
	}
}

// A second concurrent Start is rejected instead of running a second loop.
func TestProberStartTwice(t *testing.T) {
	for _, tc := range lifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := tc.setup(t)
			events := make(chan *Event, 1000)
			log := collectEvents(events)
			first := make(chan error, 1)
			go func() { first <- p.Start(events, lifecycleInterval, lifecycleTimeout) }()
			deadline := time.Now().Add(5 * time.Second)
			for log.count(SENT) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("first Start did not probe")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := p.Start(events, lifecycleInterval, lifecycleTimeout); err == nil {
				t.Error("second Start() error = nil, want an error")
			}
			stopReturns(t, p)
			if err := <-first; err != nil {
				t.Errorf("first Start() error = %v", err)
			}
			close(events)
			<-log.done
		})
	}
}
