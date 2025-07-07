package stats

import (
	"time"
	
	"github.com/servak/mping/internal/prober"
)

// 履歴エントリ
type HistoryEntry struct {
	Timestamp time.Time     `json:"timestamp"`
	RTT       time.Duration `json:"rtt"`
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
	Details   *prober.ProbeDetails `json:"details,omitempty"`
}

// 各ターゲットの履歴を管理する構造体（リングバッファ）
type TargetHistory struct {
	entries []HistoryEntry
	size    int // リングバッファサイズ
	index   int // 現在の書き込み位置
	count   int // 実際のエントリ数
}

// NewTargetHistory は新しいTargetHistoryを作成
func NewTargetHistory(size int) *TargetHistory {
	return &TargetHistory{
		entries: make([]HistoryEntry, size),
		size:    size,
		index:   0,
		count:   0,
	}
}

// AddEntry は新しい履歴エントリを追加
func (th *TargetHistory) AddEntry(entry HistoryEntry) {
	th.entries[th.index] = entry
	th.index = (th.index + 1) % th.size
	if th.count < th.size {
		th.count++
	}
}

// GetRecentEntries は最新のn件のエントリを取得（新しい順）
func (th *TargetHistory) GetRecentEntries(n int) []HistoryEntry {
	if n <= 0 || th.count == 0 {
		return []HistoryEntry{}
	}

	if n > th.count {
		n = th.count
	}

	result := make([]HistoryEntry, n)
	for i := 0; i < n; i++ {
		// 最新から順に取得
		pos := (th.index - 1 - i + th.size) % th.size
		result[i] = th.entries[pos]
	}

	return result
}

// GetEntriesSince は指定時刻以降のエントリを取得
func (th *TargetHistory) GetEntriesSince(since time.Time) []HistoryEntry {
	if th.count == 0 {
		return []HistoryEntry{}
	}

	var result []HistoryEntry
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if entry.Timestamp.After(since) || entry.Timestamp.Equal(since) {
			result = append(result, entry)
		} else {
			break // 古いエントリに到達したので終了
		}
	}

	return result
}

// GetConsecutiveFailures は連続失敗回数を取得
func (th *TargetHistory) GetConsecutiveFailures() int {
	if th.count == 0 {
		return 0
	}

	count := 0
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if !entry.Success {
			count++
		} else {
			break
		}
	}

	return count
}

// GetConsecutiveSuccesses は連続成功回数を取得
func (th *TargetHistory) GetConsecutiveSuccesses() int {
	if th.count == 0 {
		return 0
	}

	count := 0
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if entry.Success {
			count++
		} else {
			break
		}
	}

	return count
}

// GetSuccessRateInPeriod は指定期間内の成功率を取得
func (th *TargetHistory) GetSuccessRateInPeriod(duration time.Duration) float64 {
	if th.count == 0 {
		return 0.0
	}

	since := time.Now().Add(-duration)
	entries := th.GetEntriesSince(since)
	
	if len(entries) == 0 {
		return 0.0
	}

	successCount := 0
	for _, entry := range entries {
		if entry.Success {
			successCount++
		}
	}

	return float64(successCount) / float64(len(entries)) * 100.0
}

// Clear は履歴をクリア
func (th *TargetHistory) Clear() {
	th.index = 0
	th.count = 0
}