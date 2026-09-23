package trace

import (
	"context"
	"testing"
	"time"
)

func TestTraceLoopback(t *testing.T) {
	tr, err := New("127.0.0.1", Config{Interval: 50 * time.Millisecond, Timeout: 300 * time.Millisecond, NoDNS: true})
	if err != nil {
		t.Skipf("ICMP socket unavailable in this environment: %v", err)
	}
	if err := tr.Run(context.Background(), 3); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	res := tr.Snapshot()
	if !res.Reached {
		t.Skip("no echo reply from loopback (ICMP may be filtered here)")
	}
	if res.Rounds != 3 {
		t.Errorf("rounds = %d, want 3", res.Rounds)
	}
	if len(res.Hops) != 1 {
		t.Fatalf("loopback should be a single hop, got %+v", res.Hops)
	}
	h := res.Hops[0]
	if h.Addr != "127.0.0.1" || h.Recv == 0 || h.Sent < h.Recv || h.Best <= 0 || h.Best > h.Worst {
		t.Errorf("unexpected hop: %+v", h)
	}
}

func TestTraceRunStopsOnCancel(t *testing.T) {
	tr, err := New("127.0.0.1", Config{Interval: time.Hour, Timeout: time.Hour, NoDNS: true})
	if err != nil {
		t.Skipf("ICMP socket unavailable in this environment: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tr.Run(ctx, 0) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestHopLoss(t *testing.T) {
	if l := (Hop{Sent: 4, Recv: 3}).Loss(); l != 25 {
		t.Errorf("Loss() = %v, want 25", l)
	}
	if l := (Hop{}).Loss(); l != 0 {
		t.Errorf("Loss() with no probes = %v, want 0", l)
	}
}

func TestHasRateLimitedHops(t *testing.T) {
	mk := func(reached bool, hops ...Hop) Result { return Result{Reached: reached, Hops: hops} }
	lossy := Hop{Addr: "a", Sent: 4, Recv: 2}
	silent := Hop{Sent: 4}
	clean := Hop{Addr: "d", Sent: 4, Recv: 4}
	lossyDst := Hop{Addr: "d", Sent: 4, Recv: 3}

	tests := []struct {
		name string
		res  Result
		want bool
	}{
		{"intermediate loss, clean destination", mk(true, lossy, clean), true},
		{"destination also lossy (real loss)", mk(true, lossy, lossyDst), false},
		{"silent hop only (no ICMP at all)", mk(true, silent, clean), false},
		{"destination not reached", mk(false, lossy, clean), false},
		{"single hop", mk(true, clean), false},
	}
	for _, tt := range tests {
		if got := tt.res.HasRateLimitedHops(); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestProbeLimit(t *testing.T) {
	withReplies := func(ttls ...int) *Tracer {
		tr := &Tracer{hops: make([]hopStats, maxHops), maxTTL: maxHops}
		for _, ttl := range ttls {
			tr.hops[ttl-1].recv = 1
		}
		return tr
	}

	if got := withReplies().probeLimitLocked(); got != maxUnknownHops {
		t.Errorf("no replies yet: limit = %d, want %d", got, maxUnknownHops)
	}
	if got := withReplies(1, 7).probeLimitLocked(); got != 7+maxUnknownHops {
		t.Errorf("farthest reply at 7: limit = %d, want %d", got, 7+maxUnknownHops)
	}
	if got := withReplies(25).probeLimitLocked(); got != maxHops {
		t.Errorf("limit must not exceed maxHops, got %d", got)
	}

	reached := withReplies(1, 3)
	reached.reached, reached.maxTTL = true, 12
	if got := reached.probeLimitLocked(); got != 12 {
		t.Errorf("destination at 12: limit = %d, want 12", got)
	}
}
