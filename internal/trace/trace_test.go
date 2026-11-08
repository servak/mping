package trace

import (
	"context"
	"fmt"
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

// answer registers a pending probe for ttl in round and answers it with
// kind from addr, the tracer's round counter set to round
func (tr *Tracer) answer(ttl, round int, addr string, kind replyKind) {
	tr.rounds = round
	seq := uint16(round*100 + ttl)
	tr.pending[seq] = inflight{ttl: ttl, round: round, sent: testEpoch}
	tr.handle(reply{seq: seq, kind: kind}, addr, testEpoch.Add(ms(1)))
}

// routers answers TTL 1..n with Time Exceeded from r1..rn
func (tr *Tracer) routers(round, n int) {
	for ttl := 1; ttl <= n; ttl++ {
		tr.answer(ttl, round, fmt.Sprintf("r%d", ttl), kindTimeExceed)
	}
}

// One Net Unreachable from a router must not cut the path for good
func TestTransientUnreachableDoesNotEndPath(t *testing.T) {
	tr := newTestTracer()
	tr.routers(1, 1)
	tr.answer(2, 1, "r2", kindUnreachable) // transient: no route at r2
	if res := tr.Snapshot(); !res.Reached || len(res.Hops) != 2 {
		t.Fatalf("after unreachable: %+v", res)
	}

	tr.routers(2, 2) // r2 forwards again
	if tr.reached || tr.probeLimitLocked() <= 2 {
		t.Fatalf("Time Exceeded from the end TTL: reached=%v limit=%d, want probing beyond 2", tr.reached, tr.probeLimitLocked())
	}
	tr.routers(3, 3)
	tr.answer(4, 3, "dst", kindEchoReply)
	res := tr.Snapshot()
	if !res.Reached || len(res.Hops) != 4 || res.Hops[3].Addr != "dst" {
		t.Fatalf("destination behind the transient unreachable: %+v", res)
	}
	// and it stays there
	for round := 4; round < 4+2*hideAfterRounds; round++ {
		tr.routers(round, 3)
		tr.answer(4, round, "dst", kindEchoReply)
		if !tr.reached || tr.maxTTL != 4 {
			t.Fatalf("round %d: reached=%v maxTTL=%d", round, tr.reached, tr.maxTTL)
		}
	}
}

// The path getting longer is noticed: the old end TTL now sends Time Exceeded
func TestLongerPathFound(t *testing.T) {
	tr := newTestTracer()
	for round := 1; round <= 3; round++ {
		tr.routers(round, 2)
		tr.answer(3, round, "dst", kindEchoReply)
	}
	if !tr.reached || tr.maxTTL != 3 {
		t.Fatalf("reached=%v maxTTL=%d", tr.reached, tr.maxTTL)
	}
	tr.routers(4, 3) // a new router at TTL 3
	if tr.reached || tr.probeLimitLocked() <= 3 {
		t.Fatalf("reached=%v limit=%d, want probing beyond 3", tr.reached, tr.probeLimitLocked())
	}
	tr.routers(5, 4)
	tr.answer(5, 5, "dst", kindEchoReply)
	res := tr.Snapshot()
	if !res.Reached || len(res.Hops) != 5 || res.Hops[4].Addr != "dst" {
		t.Errorf("result = %+v", res)
	}
}

// Unequal-length ECMP branches (destination at 5 or 6) settle on the longer
// one instead of flipping every round
func TestUnequalECMPPathsStable(t *testing.T) {
	tr := newTestTracer()
	for round := 1; round <= 40; round++ {
		tr.routers(round, 4)
		if round%2 == 0 {
			tr.answer(5, round, "dst", kindUnreachable)
		} else {
			tr.answer(5, round, "r5b", kindTimeExceed)
		}
		tr.answer(6, round, "dst", kindUnreachable)
		if round > 1 && (!tr.reached || tr.maxTTL != 6) {
			t.Fatalf("round %d: reached=%v maxTTL=%d, want 6", round, tr.reached, tr.maxTTL)
		}
	}
}

// A shorter path is adopted once the old routers have been silent a while
func TestShorterPathFound(t *testing.T) {
	tr := newTestTracer()
	for round := 1; round <= 5; round++ {
		tr.routers(round, 3)
		tr.answer(4, round, "dst", kindEchoReply)
	}
	for round := 6; round <= 6+staleAfterRounds; round++ {
		tr.routers(round, 1)
		for ttl := 2; ttl <= 4; ttl++ {
			tr.answer(ttl, round, "dst", kindEchoReply)
		}
	}
	if !tr.reached || tr.maxTTL != 2 {
		t.Errorf("reached=%v maxTTL=%d, want 2", tr.reached, tr.maxTTL)
	}
}
