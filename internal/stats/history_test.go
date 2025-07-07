package stats

import (
	"testing"
	"time"
	
	"github.com/servak/mping/internal/prober"
)

func TestTargetHistory(t *testing.T) {
	th := NewTargetHistory(3)

	// Test initial state
	if th.GetConsecutiveFailures() != 0 {
		t.Errorf("Expected 0 consecutive failures, got %d", th.GetConsecutiveFailures())
	}
	if th.GetConsecutiveSuccesses() != 0 {
		t.Errorf("Expected 0 consecutive successes, got %d", th.GetConsecutiveSuccesses())
	}

	now := time.Now()

	// Add success entry
	th.AddEntry(HistoryEntry{
		Timestamp: now,
		RTT:       100 * time.Millisecond,
		Success:   true,
		Details: &prober.ProbeDetails{
			ProbeType: "icmp",
			ICMP: &prober.ICMPDetails{
				Sequence: 1,
				TTL:      64,
				DataSize: 64,
			},
		},
	})

	// Test consecutive successes
	if th.GetConsecutiveSuccesses() != 1 {
		t.Errorf("Expected 1 consecutive success, got %d", th.GetConsecutiveSuccesses())
	}

	// Add failure entry
	th.AddEntry(HistoryEntry{
		Timestamp: now.Add(time.Second),
		RTT:       0,
		Success:   false,
		Error:     "timeout",
	})

	// Test consecutive failures
	if th.GetConsecutiveFailures() != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", th.GetConsecutiveFailures())
	}
	if th.GetConsecutiveSuccesses() != 0 {
		t.Errorf("Expected 0 consecutive successes, got %d", th.GetConsecutiveSuccesses())
	}

	// Get recent entries
	recent := th.GetRecentEntries(2)
	if len(recent) != 2 {
		t.Errorf("Expected 2 recent entries, got %d", len(recent))
	}
	if recent[0].Success != false {
		t.Errorf("Expected first recent entry to be failure, got success")
	}
	if recent[1].Success != true {
		t.Errorf("Expected second recent entry to be success, got failure")
	}

	// Test ring buffer behavior
	th.AddEntry(HistoryEntry{
		Timestamp: now.Add(2 * time.Second),
		RTT:       200 * time.Millisecond,
		Success:   true,
	})
	th.AddEntry(HistoryEntry{
		Timestamp: now.Add(3 * time.Second),
		RTT:       300 * time.Millisecond,
		Success:   true,
	})

	// Get entries exceeding maximum size
	allRecent := th.GetRecentEntries(5)
	if len(allRecent) != 3 {
		t.Errorf("Expected 3 recent entries (max size), got %d", len(allRecent))
	}
}

func TestMetricsWithHistory(t *testing.T) {
	mm := NewMetricsManager()
	host := "example.com"

	// Record success
	details := &prober.ProbeDetails{
		ProbeType: "icmp",
		ICMP: &prober.ICMPDetails{
			Sequence: 1,
			TTL:      64,
			DataSize: 64,
		},
	}
	mm.SuccessWithDetails(host, 50*time.Millisecond, time.Now(), details)

	// メトリクスを取得
	metrics := mm.GetMetrics(host)
	if metrics.GetTotal() != 0 {
		t.Errorf("Expected total to be 0 (Sent not called), got %d", metrics.GetTotal())
	}
	if metrics.GetSuccessful() != 1 {
		t.Errorf("Expected successful to be 1, got %d", metrics.GetSuccessful())
	}

	// 履歴を取得
	history := metrics.GetRecentHistory(1)
	if len(history) != 1 {
		t.Errorf("Expected 1 history entry, got %d", len(history))
	}
	if !history[0].Success {
		t.Errorf("Expected history entry to be success")
	}
	if history[0].Details == nil {
		t.Errorf("Expected history entry to have details")
	}
	if history[0].Details.ProbeType != "icmp" {
		t.Errorf("Expected probe type to be 'icmp', got %s", history[0].Details.ProbeType)
	}

	// Record failure
	mm.Failed(host, time.Now(), "timeout")

	// Test consecutive failures
	if metrics.GetConsecutiveFailures() != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", metrics.GetConsecutiveFailures())
	}
}

func TestSuccessRateInPeriod(t *testing.T) {
	th := NewTargetHistory(10)
	now := time.Now()

	// Create test data with timestamps close to current time
	// Add 5 successes
	for i := 0; i < 5; i++ {
		th.AddEntry(HistoryEntry{
			Timestamp: now.Add(time.Duration(-i-10) * time.Second),
			RTT:       100 * time.Millisecond,
			Success:   true,
		})
	}
	
	// Add 5 failures
	for i := 0; i < 5; i++ {
		th.AddEntry(HistoryEntry{
			Timestamp: now.Add(time.Duration(-i-1) * time.Second),
			RTT:       0,
			Success:   false,
			Error:     "timeout",
		})
	}

	// Overall success rate (50%)
	rate := th.GetSuccessRateInPeriod(time.Hour)
	if rate != 50.0 {
		t.Errorf("Expected 50%% success rate, got %f%%", rate)
	}

	// Success rate for recent 6 seconds (only latest failure entries)
	recentRate := th.GetSuccessRateInPeriod(6 * time.Second)
	if recentRate != 0.0 {
		t.Errorf("Expected 0%% success rate for recent period, got %f%%", recentRate)
	}
}