package stats

import (
	"testing"
	"time"
	
	"github.com/servak/mping/internal/prober"
)

func TestTargetHistory(t *testing.T) {
	th := NewTargetHistory(3)

	// 初期状態のテスト
	if th.GetConsecutiveFailures() != 0 {
		t.Errorf("Expected 0 consecutive failures, got %d", th.GetConsecutiveFailures())
	}
	if th.GetConsecutiveSuccesses() != 0 {
		t.Errorf("Expected 0 consecutive successes, got %d", th.GetConsecutiveSuccesses())
	}

	now := time.Now()

	// 成功エントリを追加
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

	// 連続成功数をテスト
	if th.GetConsecutiveSuccesses() != 1 {
		t.Errorf("Expected 1 consecutive success, got %d", th.GetConsecutiveSuccesses())
	}

	// 失敗エントリを追加
	th.AddEntry(HistoryEntry{
		Timestamp: now.Add(time.Second),
		RTT:       0,
		Success:   false,
		Error:     "timeout",
	})

	// 連続失敗数をテスト
	if th.GetConsecutiveFailures() != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", th.GetConsecutiveFailures())
	}
	if th.GetConsecutiveSuccesses() != 0 {
		t.Errorf("Expected 0 consecutive successes, got %d", th.GetConsecutiveSuccesses())
	}

	// 最新エントリを取得
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

	// リングバッファの動作をテスト
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

	// 最大サイズを超えたエントリを取得
	allRecent := th.GetRecentEntries(5)
	if len(allRecent) != 3 {
		t.Errorf("Expected 3 recent entries (max size), got %d", len(allRecent))
	}
}

func TestMetricsWithHistory(t *testing.T) {
	mm := NewMetricsManager()
	host := "example.com"

	// 成功を記録
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

	// 失敗を記録
	mm.Failed(host, time.Now(), "timeout")

	// 連続失敗数をテスト
	if metrics.GetConsecutiveFailures() != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", metrics.GetConsecutiveFailures())
	}
}

func TestSuccessRateInPeriod(t *testing.T) {
	th := NewTargetHistory(10)
	now := time.Now()

	// 現在時刻に近い時刻でテストデータを作成
	// 5つの成功を追加
	for i := 0; i < 5; i++ {
		th.AddEntry(HistoryEntry{
			Timestamp: now.Add(time.Duration(-i-10) * time.Second),
			RTT:       100 * time.Millisecond,
			Success:   true,
		})
	}
	
	// 5つの失敗を追加
	for i := 0; i < 5; i++ {
		th.AddEntry(HistoryEntry{
			Timestamp: now.Add(time.Duration(-i-1) * time.Second),
			RTT:       0,
			Success:   false,
			Error:     "timeout",
		})
	}

	// 全期間の成功率（50%）
	rate := th.GetSuccessRateInPeriod(time.Hour)
	if rate != 50.0 {
		t.Errorf("Expected 50%% success rate, got %f%%", rate)
	}

	// 直近6秒間の成功率（最新の失敗エントリのみ）
	recentRate := th.GetSuccessRateInPeriod(6 * time.Second)
	if recentRate != 0.0 {
		t.Errorf("Expected 0%% success rate for recent period, got %f%%", recentRate)
	}
}