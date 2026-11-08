package prober

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProber runs until Stop; its Stop takes stopDelay, like an ICMP prober
// waiting for the replies of its last round.
type fakeProber struct {
	loop      *runLoop
	stopDelay time.Duration
	started   atomic.Bool
	stopped   atomic.Bool
}

func newFakeProber(stopDelay time.Duration) *fakeProber {
	return &fakeProber{loop: newRunLoop(), stopDelay: stopDelay}
}

func (p *fakeProber) Accept(string) error { return nil }

func (p *fakeProber) Start(r chan *Event, _, _ time.Duration) error {
	ok, err := p.loop.begin()
	if !ok {
		return err
	}
	defer p.loop.finish()
	p.started.Store(true)
	<-p.loop.stopping()
	time.Sleep(p.stopDelay)
	return nil
}

func (p *fakeProber) Stop() {
	p.loop.stop()
	p.stopped.Store(true)
}

func newFakeManager(probers ...Prober) *probeManager {
	pm := NewProbeManager(map[string]*ProberConfig{"http": {Probe: HTTP, HTTP: &HTTPConfig{}}}, "http").(*probeManager)
	for i, p := range probers {
		pm.probers[string(rune('a'+i))] = p
	}
	return pm
}

// eventsClosed reports whether the event channel is closed within d.
func eventsClosed(pm ProbeManager, d time.Duration) bool {
	timer := time.After(d)
	for {
		select {
		case _, ok := <-pm.Events():
			if !ok {
				return true
			}
		case <-timer:
			return false
		}
	}
}

// runAsync starts Run and returns a channel with its result.
func runAsync(pm ProbeManager, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- pm.Run(ctx, time.Hour, time.Second) }()
	return done
}

// Stop before Run stops the probers and closes the event channel, and the
// later Run returns without starting anything.
func TestProbeManagerStopBeforeRun(t *testing.T) {
	fp := newFakeProber(0)
	pm := newFakeManager(fp)
	pm.Stop()
	if !fp.stopped.Load() {
		t.Error("Stop before Run did not stop the probers")
	}
	if !eventsClosed(pm, time.Second) {
		t.Fatal("Stop before Run did not close the event channel")
	}
	select {
	case err := <-runAsync(pm, context.Background()):
		if err != nil {
			t.Errorf("Run() after Stop error = %v", err)
		}
	case <-time.After(stopBudget):
		t.Fatal("Run after Stop did not return")
	}
	if fp.started.Load() {
		t.Error("Run after Stop started a prober")
	}
	pm.Stop() // second Stop must not panic
}

// A manager runs once: a second Run is rejected instead of closing the
// event channel twice.
func TestProbeManagerRunTwice(t *testing.T) {
	pm := newFakeManager(newFakeProber(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(pm, ctx)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if !eventsClosed(pm, time.Second) {
		t.Fatal("event channel not closed after Run")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second Run panicked: %v", r)
		}
	}()
	if err := pm.Run(ctx2, time.Hour, time.Second); err == nil {
		t.Error("second Run() error = nil, want an error")
	}
	pm.Stop()
}

// Run without probers fails but leaves the manager usable.
func TestProbeManagerRunWithoutProbers(t *testing.T) {
	pm := newFakeManager()
	if err := pm.Run(context.Background(), time.Hour, time.Second); err == nil {
		t.Fatal("Run() without probers error = nil")
	}
	if err := pm.AddTargets("http://127.0.0.1/"); err != nil {
		t.Errorf("AddTargets after failed Run: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pm.Run(ctx, time.Hour, time.Second); err != nil {
		t.Errorf("Run() after AddTargets error = %v", err)
	}
}

// The probers are stopped concurrently, so Stop takes as long as the
// slowest prober rather than their sum.
func TestProbeManagerStopsProbersConcurrently(t *testing.T) {
	const delay = 300 * time.Millisecond
	pm := newFakeManager(newFakeProber(delay), newFakeProber(delay), newFakeProber(delay))
	done := runAsync(pm, context.Background())
	deadline := time.Now().Add(stopBudget)
	for {
		all := true
		for _, p := range pm.probers {
			all = all && p.(*fakeProber).started.Load()
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probers did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	begin := time.Now()
	pm.Stop()
	if took := time.Since(begin); took > 2*delay {
		t.Errorf("Stop took %v, want about %v (probers stopped one by one?)", took, delay)
	}
	if err := <-done; err != nil {
		t.Errorf("Run() error = %v", err)
	}
}

// Stop racing with the start of Run must neither hang nor misuse the
// WaitGroup, whichever wins.
func TestProbeManagerStopRacesRun(t *testing.T) {
	for range 50 {
		fp := newFakeProber(0)
		pm := newFakeManager(fp)
		done := runAsync(pm, context.Background())
		pm.Stop()
		select {
		case <-done:
		case <-time.After(stopBudget):
			t.Fatal("Run did not return after Stop")
		}
		if !eventsClosed(pm, time.Second) {
			t.Fatal("event channel not closed")
		}
	}
}
