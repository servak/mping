package stats

import (
	"sync"
	"testing"

	"github.com/servak/mping/internal/prober"
)

func TestOnFailureHookReceivesDisplayName(t *testing.T) {
	var mu sync.Mutex
	var got []string
	mm := NewMetricsManagerWithOptions(Options{OnFailure: func(name string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, name)
	}})

	ch := make(chan *prober.Event, 5)
	ch <- &prober.Event{Key: "icmpv4:192.0.2.1", DisplayName: "host-a", Result: prober.REGISTER}
	ch <- &prober.Event{Key: "icmpv4:192.0.2.1", Result: prober.SUCCESS}
	ch <- &prober.Event{Key: "icmpv4:192.0.2.1", Result: prober.TIMEOUT}
	ch <- &prober.Event{Key: "icmpv4:192.0.2.1", Result: prober.FAILED, Message: "unreachable"}
	close(ch)
	<-mm.Subscribe(ch)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "host-a" || got[1] != "host-a" {
		t.Errorf("OnFailure calls = %v, want [host-a host-a]", got)
	}
	if f := mm.(*metricsManager).GetMetrics("icmpv4:192.0.2.1").GetFailed(); f != 2 {
		t.Errorf("GetFailed() = %d, want 2", f)
	}
}

func TestOnFailureHookRunsOutsideLock(t *testing.T) {
	var mm MetricsManager
	// The hook reads metrics; it would deadlock if called under mm.mu
	mm = NewMetricsManagerWithOptions(Options{OnFailure: func(string) {
		mm.SortBy(Host, true)
	}})
	ch := make(chan *prober.Event, 2)
	ch <- &prober.Event{Key: "x", DisplayName: "x", Result: prober.REGISTER}
	ch <- &prober.Event{Key: "x", Result: prober.FAILED}
	close(ch)
	<-mm.Subscribe(ch)
}

func TestNoFailureHookStillCounts(t *testing.T) {
	mm := NewMetricsManager()
	ch := make(chan *prober.Event, 2)
	ch <- &prober.Event{Key: "x", DisplayName: "x", Result: prober.REGISTER}
	ch <- &prober.Event{Key: "x", Result: prober.FAILED}
	close(ch)
	<-mm.Subscribe(ch)
	if f := mm.(*metricsManager).GetMetrics("x").GetFailed(); f != 1 {
		t.Errorf("GetFailed() = %d, want 1", f)
	}
}
