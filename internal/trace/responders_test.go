package trace

import (
	"fmt"
	"testing"
	"time"
)

// newTestTracer builds a Tracer that is driven only through handle()
func newTestTracer() *Tracer {
	return &Tracer{
		cfg:     Config{NoDNS: true},
		pending: map[uint16]inflight{},
		hops:    make([]hopStats, maxHops),
		maxTTL:  maxHops,
		names:   map[string]string{},
	}
}

var testEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// inject registers a pending probe for ttl/round and answers it from addr
// after rtt, the way receive() would. The tracer's round counter follows.
func (tr *Tracer) inject(ttl, round int, addr string, rtt time.Duration) {
	seq := uint16(round*100 + ttl)
	tr.pending[seq] = inflight{ttl: ttl, round: round, sent: testEpoch}
	tr.handle(reply{seq: seq, kind: kindTimeExceed}, addr, testEpoch.Add(rtt))
	if round > tr.rounds {
		tr.rounds = round
	}
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func TestMultipleRespondersOnOneTTL(t *testing.T) {
	tr := newTestTracer()
	tr.inject(2, 1, "10.0.0.1", ms(1))
	tr.inject(2, 2, "10.0.0.5", ms(4))
	tr.inject(2, 3, "10.0.0.1", ms(3))
	tr.inject(2, 4, "10.0.0.1", ms(2))

	hop := tr.Snapshot().Hops[1]
	if hop.Recv != 4 || hop.Sent != 4 {
		t.Fatalf("hop totals = sent %d recv %d, want 4/4", hop.Sent, hop.Recv)
	}
	if len(hop.Responders) != 2 || hop.Others != 1 {
		t.Fatalf("responders = %+v, others = %d", hop.Responders, hop.Others)
	}
	a, b := hop.Responders[0], hop.Responders[1]
	if a.Addr != "10.0.0.1" || b.Addr != "10.0.0.5" {
		t.Errorf("first-seen order broken: %s, %s", a.Addr, b.Addr)
	}
	if a.Recv != 3 || a.Last != ms(2) || a.Avg != ms(2) || a.Best != ms(1) || a.Worst != ms(3) {
		t.Errorf("responder a = %+v", a)
	}
	if b.Recv != 1 || b.Last != ms(4) || b.Avg != ms(4) || b.Best != ms(4) || b.Worst != ms(4) {
		t.Errorf("responder b = %+v", b)
	}
	if hop.Addr != "10.0.0.1" {
		t.Errorf("dominant = %s", hop.Addr)
	}
	// hop-level RTTs cover all responders
	if hop.Best != ms(1) || hop.Worst != ms(4) || hop.Avg != 2500*time.Microsecond {
		t.Errorf("hop rtt = %v/%v/%v", hop.Best, hop.Avg, hop.Worst)
	}
}

func TestDominantResponder(t *testing.T) {
	tr := newTestTracer()
	tr.inject(1, 1, "a", ms(1))
	tr.inject(1, 2, "b", ms(1))
	if got := tr.Snapshot().Hops[0].Addr; got != "a" {
		t.Errorf("tie: dominant = %s, want earliest seen a", got)
	}
	// b replies last and leads: dominant follows the count, not recency
	tr.inject(1, 3, "b", ms(1))
	if got := tr.Snapshot().Hops[0].Addr; got != "b" {
		t.Errorf("dominant = %s, want b", got)
	}
	// a replies last but b still leads: no flicker
	tr.inject(1, 4, "a", ms(1))
	tr.inject(1, 5, "a", ms(1))
	tr.inject(1, 6, "b", ms(1))
	tr.inject(1, 7, "a", ms(1))
	if got := tr.Snapshot().Hops[0].Addr; got != "a" {
		t.Errorf("dominant = %s, want a (4 vs 3)", got)
	}
	tr.inject(1, 8, "b", ms(1)) // 4 vs 4 again: earliest seen wins
	if got := tr.Snapshot().Hops[0].Addr; got != "a" {
		t.Errorf("tie: dominant = %s, want a", got)
	}
}

func TestStaleAndHiddenThresholds(t *testing.T) {
	tr := newTestTracer()
	tr.inject(1, 1, "old", ms(9))
	for r := 1; r <= 100; r++ {
		tr.inject(1, r, "main", ms(1))
	}
	cases := []struct {
		rounds    int
		visible   bool
		wantStale bool
	}{
		{1 + staleAfterRounds, true, false},
		{1 + staleAfterRounds + 1, true, true},
		{1 + hideAfterRounds, true, true},
		{1 + hideAfterRounds + 1, false, false},
	}
	for _, c := range cases {
		tr.rounds = c.rounds
		hop := tr.Snapshot().Hops[0]
		var old *Responder
		for i := range hop.Responders {
			if hop.Responders[i].Addr == "old" {
				old = &hop.Responders[i]
			}
		}
		if (old != nil) != c.visible {
			t.Fatalf("round %d: visible = %v, want %v", c.rounds, old != nil, c.visible)
		}
		if old != nil && old.Stale != c.wantStale {
			t.Errorf("round %d: stale = %v, want %v", c.rounds, old.Stale, c.wantStale)
		}
		if hop.Recv != 101 {
			t.Errorf("round %d: hidden responders must stay in hop totals, recv = %d", c.rounds, hop.Recv)
		}
	}
}

func TestResponderCap(t *testing.T) {
	tr := newTestTracer()
	for i := 0; i < maxRespondersPerHop+4; i++ {
		tr.inject(1, 1+i/4, fmt.Sprintf("10.0.0.%d", i), ms(1)) // none stale yet
	}
	hop := tr.Snapshot().Hops[0]
	if len(hop.Responders) != maxRespondersPerHop {
		t.Errorf("responders = %d, want cap %d", len(hop.Responders), maxRespondersPerHop)
	}
	if hop.Recv != maxRespondersPerHop+4 || hop.Sent != hop.Recv {
		t.Errorf("hop totals must include dropped responders: sent %d recv %d", hop.Sent, hop.Recv)
	}
	// known responders keep counting once the cap is reached
	tr.inject(1, 6, "10.0.0.0", ms(1))
	if got := tr.Snapshot().Hops[0].Responders[0].Recv; got != 2 {
		t.Errorf("first responder recv = %d, want 2", got)
	}
}

func TestLateReplyIgnored(t *testing.T) {
	tr := newTestTracer()
	tr.inject(1, 1, "a", ms(1))
	tr.handle(reply{seq: 4242, kind: kindTimeExceed}, "b", testEpoch) // no pending entry
	hop := tr.Snapshot().Hops[0]
	if hop.Recv != 1 || len(hop.Responders) != 1 {
		t.Errorf("late reply was counted: %+v", hop)
	}
}

func findResponder(hop Hop, addr string) *Responder {
	for i := range hop.Responders {
		if hop.Responders[i].Addr == addr {
			return &hop.Responders[i]
		}
	}
	return nil
}

func TestSparseReplyHopStaysVisible(t *testing.T) {
	tr := newTestTracer()
	tr.inject(1, 1, "gw", ms(4))
	tr.rounds = 51
	hop := tr.Snapshot().Hops[0]
	if hop.Addr != "gw" || len(hop.Responders) != 1 {
		t.Fatalf("hop = %+v", hop)
	}
	if !hop.Responders[0].Stale {
		t.Errorf("lone silent responder should be stale")
	}
}

func TestPathChangeHidesOldResponder(t *testing.T) {
	tr := newTestTracer()
	for r := 1; r <= 5; r++ {
		tr.inject(1, r, "A", ms(1))
	}
	for r := 6; r <= 50; r++ {
		tr.inject(1, r, "B", ms(1))
	}
	hop := tr.Snapshot().Hops[0]
	if findResponder(hop, "A") != nil || findResponder(hop, "B") == nil {
		t.Errorf("responders = %+v", hop.Responders)
	}
}

func TestMostRecentSilentResponderKept(t *testing.T) {
	tr := newTestTracer()
	tr.inject(1, 1, "A", ms(1))
	tr.inject(1, 5, "B", ms(1))
	tr.rounds = 60
	hop := tr.Snapshot().Hops[0]
	if findResponder(hop, "A") != nil {
		t.Errorf("older responder should be hidden")
	}
	if b := findResponder(hop, "B"); b == nil || !b.Stale || hop.Addr != "B" {
		t.Errorf("hop = %+v", hop)
	}
}

// Responders that went silent long ago must not block new ones once the
// per-hop table is full
func TestResponderCapEvictsSilent(t *testing.T) {
	tr := newTestTracer()
	for i := 0; i < maxRespondersPerHop; i++ {
		tr.inject(1, 1+i/4, fmt.Sprintf("10.0.0.%d", i), ms(1))
	}
	for round := 50; round <= 52; round++ {
		tr.inject(1, round, "10.9.9.9", ms(2))
	}
	hop := tr.Snapshot().Hops[0]
	if hop.Addr != "10.9.9.9" {
		t.Errorf("hop addr = %q, want the new responder; responders %+v", hop.Addr, hop.Responders)
	}
	if r := findResponder(hop, "10.9.9.9"); r == nil || r.Recv != 3 {
		t.Errorf("new responder = %+v", r)
	}
	if n := len(tr.hops[0].order); n > maxRespondersPerHop {
		t.Errorf("%d responders tracked, cap %d", n, maxRespondersPerHop)
	}
	// stale (not yet hidden) responders make room too, oldest first
	tr = newTestTracer()
	for i := 0; i < maxRespondersPerHop; i++ {
		tr.inject(1, 1+i, fmt.Sprintf("10.0.0.%d", i), ms(1))
	}
	tr.inject(1, 1+maxRespondersPerHop+staleAfterRounds, "10.9.9.9", ms(1))
	hop = tr.Snapshot().Hops[0]
	if findResponder(hop, "10.9.9.9") == nil || findResponder(hop, "10.0.0.0") != nil || findResponder(hop, "10.0.0.15") == nil {
		t.Errorf("responders = %+v", hop.Responders)
	}
}
