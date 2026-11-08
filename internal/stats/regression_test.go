package stats

import (
	"slices"
	"testing"
	"time"
)

func TestResetDropsStaleResults(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	sent := time.Now().Add(-time.Second)
	mm.Sent("a", time.Now())
	time.Sleep(2 * time.Millisecond)
	mm.ResetAllMetrics()
	mm.SuccessWithDetails("a", time.Millisecond, sent, nil)
	mm.Failed("a", sent, "late")
	m := mm.GetMetrics("a")
	if m.GetTotal() != 0 || m.GetSuccessful() != 0 || m.GetFailed() != 0 {
		t.Errorf("stale results counted: %d/%d/%d", m.GetTotal(), m.GetSuccessful(), m.GetFailed())
	}
	if len(m.GetRecentHistory(10)) != 0 {
		t.Error("stale result entered history")
	}
	mm.SuccessWithDetails("a", time.Millisecond, time.Now(), nil)
	if mm.GetMetrics("a").GetSuccessful() != 1 {
		t.Error("fresh result after reset must be counted")
	}
}

func TestHistoryOrderedBySentTime(t *testing.T) {
	h := NewTargetHistory(5)
	base := time.Now().Add(-time.Minute)
	h.AddEntry(HistoryEntry{Timestamp: base.Add(2 * time.Second), Success: true})
	h.AddEntry(HistoryEntry{Timestamp: base, Success: false}) // late timeout of older probe
	if got := h.GetConsecutiveSuccesses(); got != 1 {
		t.Errorf("consecutive successes = %d, want 1", got)
	}
	if got := h.GetConsecutiveFailures(); got != 0 {
		t.Errorf("consecutive failures = %d, want 0", got)
	}
	e := h.GetRecentEntries(2)
	if !e[0].Timestamp.After(e[1].Timestamp) {
		t.Error("entries not newest-first by sent time")
	}
	// full buffer: insertion keeps order and size
	for i := 3; i < 8; i++ {
		h.AddEntry(HistoryEntry{Timestamp: base.Add(time.Duration(i) * time.Second), Success: true})
	}
	h.AddEntry(HistoryEntry{Timestamp: base.Add(4500 * time.Millisecond), Success: false})
	e = h.GetRecentEntries(10)
	if len(e) != 5 {
		t.Fatalf("len = %d", len(e))
	}
	for i := 1; i < len(e); i++ {
		if e[i].Timestamp.After(e[i-1].Timestamp) {
			t.Errorf("order broken at %d", i)
		}
	}
}

func TestLastTimesKeepNewest(t *testing.T) {
	m := newMetrics("a", 5, 0)
	t1 := time.Now()
	t0 := t1.Add(-time.Second)
	m.Success(time.Millisecond, t1)
	m.Success(time.Millisecond, t0)
	if !m.LastSuccTime.Equal(t1) {
		t.Error("LastSuccTime overwritten by older result")
	}
	m.Fail(t1, "new")
	m.Fail(t0, "old")
	if !m.LastFailTime.Equal(t1) || m.LastFailDetail != "new" {
		t.Error("LastFailTime/Detail overwritten by older result")
	}
}

func TestMinimumRTTZeroSample(t *testing.T) {
	m := newMetrics("a", 5, 0)
	m.Success(0, time.Now())
	m.Success(5*time.Millisecond, time.Now())
	if m.MinimumRTT != 0 {
		t.Errorf("MinimumRTT = %v, want 0", m.MinimumRTT)
	}
}

func TestSortDeterministicTies(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	for _, k := range []string{"k3", "k1", "k2", "k5", "k4"} {
		mm.autoRegister(k, "same", "")
	}
	var first []string
	// order must follow key order for equal names/values
	mm.metrics["k1"].ProbeType = "p1"
	mm.metrics["k2"].ProbeType = "p2"
	mm.metrics["k3"].ProbeType = "p3"
	mm.metrics["k4"].ProbeType = "p4"
	mm.metrics["k5"].ProbeType = "p5"
	for i := 0; i < 30; i++ {
		var got []string
		for _, r := range mm.SortBy(Sent, i%2 == 0) {
			got = append(got, r.GetProbeType())
		}
		if first == nil {
			first = got
		}
		want := []string{"p1", "p2", "p3", "p4", "p5"}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: got %v, want %v", i, got, want)
			}
		}
	}
}

func TestNewMetricsZeroHistorySize(t *testing.T) {
	m := NewMetrics("a", 0)
	h := NewTargetHistory(-1)
	h.AddEntry(HistoryEntry{Timestamp: time.Now()})
	if len(h.GetRecentEntries(5)) != 1 {
		t.Error("expected one entry")
	}
	_ = m
}

func TestSentProcessedAfterResetIsDropped(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	sent := time.Now()
	time.Sleep(2 * time.Millisecond)
	mm.getMetrics("h")
	mm.ResetAllMetrics()
	// SENT and its result were queued before the reset, processed after it
	mm.Sent("h", sent)
	mm.SuccessWithDetails("h", time.Millisecond, sent, nil)
	m := mm.GetMetrics("h")
	if m.GetTotal() != 0 || m.GetSuccessful() != 0 || m.GetFailed() != 0 {
		t.Errorf("stale SENT counted: %d/%d/%d", m.GetTotal(), m.GetSuccessful(), m.GetFailed())
	}
	mm.Sent("h", time.Now())
	if mm.GetMetrics("h").GetTotal() != 1 {
		t.Error("fresh SENT after reset must be counted")
	}
}

func TestLastRTTKeepsNewest(t *testing.T) {
	m := newMetrics("x", 10, 0)
	now := time.Now()
	m.Success(5*time.Millisecond, now)
	m.Success(500*time.Millisecond, now.Add(-time.Second))
	if m.LastRTT != 5*time.Millisecond {
		t.Errorf("LastRTT = %v, want 5ms", m.LastRTT)
	}
	// equal send time: newest-or-equal wins, for success and failure alike
	m.Success(7*time.Millisecond, now)
	if m.LastRTT != 7*time.Millisecond {
		t.Errorf("LastRTT = %v, want 7ms on tie", m.LastRTT)
	}
	m.Fail(now, "a")
	m.Fail(now, "b")
	if m.LastFailDetail != "b" {
		t.Errorf("LastFailDetail = %q, want b on tie", m.LastFailDetail)
	}
}

func TestInsertOrderedNoAllocAndOrder(t *testing.T) {
	for _, size := range []int{1, 2, 3, 5, 8} {
		base := time.Now()
		h := NewTargetHistory(size)
		added := 0
		// pseudo-random out-of-order timestamps, wrapping the ring several times
		for i := 0; i < 40; i++ {
			ts := base.Add(time.Duration((i*7)%13) * time.Second)
			h.AddEntry(HistoryEntry{Timestamp: ts})
			added++
			e := h.GetRecentEntries(size)
			for j := 1; j < len(e); j++ {
				if e[j].Timestamp.After(e[j-1].Timestamp) {
					t.Fatalf("size %d step %d: order broken", size, i)
				}
			}
			if want := min(added, size); len(e) != want {
				t.Fatalf("size %d step %d: len %d want %d", size, i, len(e), want)
			}
		}
	}
	h := NewTargetHistory(100)
	base := time.Now()
	for i := 0; i < 100; i++ {
		h.AddEntry(HistoryEntry{Timestamp: base.Add(time.Duration(i) * time.Second)})
	}
	allocs := testing.AllocsPerRun(100, func() {
		h.AddEntry(HistoryEntry{Timestamp: base.Add(50*time.Second + time.Millisecond)})
	})
	if allocs != 0 {
		t.Errorf("late insert allocated %v times", allocs)
	}
}

func TestInsertOrderedFullOlderThanAll(t *testing.T) {
	th := NewTargetHistory(3)
	base := time.Now()
	for i := 1; i <= 3; i++ {
		th.AddEntry(HistoryEntry{Timestamp: base.Add(time.Duration(i) * time.Second), RTT: time.Duration(i)})
	}
	th.AddEntry(HistoryEntry{Timestamp: base, RTT: 99}) // older than all: dropped
	th.AddEntry(HistoryEntry{Timestamp: base.Add(1500 * time.Millisecond), RTT: 15})
	var got []time.Duration
	for _, e := range th.GetRecentEntries(3) {
		got = append(got, e.RTT)
	}
	if !slices.Equal(got, []time.Duration{3, 2, 15}) {
		t.Errorf("got %v, want [3 2 15]", got)
	}
}

func TestSortByTieBreakNameThenKey(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	mm.autoRegister("k2", "b", "p2")
	mm.autoRegister("k1", "b", "p1")
	mm.autoRegister("k3", "a", "p3")
	for _, asc := range []bool{true, false} {
		var got []string
		for _, r := range mm.SortBy(Sent, asc) {
			got = append(got, r.GetProbeType())
		}
		if !slices.Equal(got, []string{"p3", "p1", "p2"}) {
			t.Errorf("asc=%v got %v", asc, got)
		}
	}
}
