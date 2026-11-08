package stats

import (
	"testing"
	"time"
)

func TestResetDropsStaleResults(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	sent := time.Now().Add(-time.Second)
	mm.Sent("a")
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
