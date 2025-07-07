package stats

import (
	"time"
	
	"github.com/servak/mping/internal/prober"
)

// ターゲット別のメトリクス読み取り用インターフェース
type MetricsReader interface {
	// 基本統計情報
	GetName() string
	GetTotal() int
	GetSuccessful() int
	GetFailed() int
	GetLoss() float64
	GetLastRTT() time.Duration
	GetAverageRTT() time.Duration
	GetMinimumRTT() time.Duration
	GetMaximumRTT() time.Duration
	GetLastSuccTime() time.Time
	GetLastFailTime() time.Time
	GetLastFailDetail() string

	// 履歴情報
	GetRecentHistory(n int) []HistoryEntry
	GetHistorySince(since time.Time) []HistoryEntry
	GetConsecutiveFailures() int
	GetConsecutiveSuccesses() int
	GetSuccessRateInPeriod(duration time.Duration) float64
}

// メトリクス管理全体のインターフェース
type MetricsManagerInterface interface {
	// 基本操作
	GetMetrics(target string) MetricsReader
	GetAllTargets() []string
	ResetAllMetrics()

	// 統計情報登録
	Success(target string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails)
	Failed(target string, sentTime time.Time, msg string)
	Sent(target string)

	// 履歴機能
	GetTargetHistory(target string, n int) []HistoryEntry
	GetAllTargetsRecentHistory(n int) map[string][]HistoryEntry

	// ソート機能
	SortBy(k Key, ascending bool) []MetricsReader
}

// 履歴専用インターフェース
type HistoryManagerInterface interface {
	AddSuccessEntry(target string, timestamp time.Time, rtt time.Duration, details *prober.ProbeDetails)
	AddFailureEntry(target string, timestamp time.Time, error string)
	GetHistory(target string, n int) []HistoryEntry
	GetHistorySince(target string, since time.Time) []HistoryEntry
	ClearHistory(target string)
	ClearAllHistory()
}