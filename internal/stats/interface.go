package stats

import (
	"time"
	
	"github.com/servak/mping/internal/prober"
)

// BasicMetrics provides basic statistics for display and sorting
type BasicMetrics interface {
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
}

// DetailedMetrics extends BasicMetrics with history and detailed analysis
type DetailedMetrics interface {
	BasicMetrics
	GetRecentHistory(n int) []HistoryEntry
	GetConsecutiveFailures() int
	GetConsecutiveSuccesses() int
	GetSuccessRateInPeriod(duration time.Duration) float64
}

// MetricsReader provides complete read access to metrics (for backward compatibility)
type MetricsReader interface {
	DetailedMetrics
	GetHistorySince(since time.Time) []HistoryEntry
}

// MetricsProvider provides external API for metrics access
type MetricsProvider interface {
	SortByWithReader(k Key, ascending bool) []MetricsReader
	GetMetrics(target string) MetricsReader
}

// MetricsSystemManager provides system-level operations
type MetricsSystemManager interface {
	ResetAllMetrics()
}

// MetricsEventRecorder handles internal event recording
type MetricsEventRecorder interface {
	Success(target string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails)
	Failed(target string, sentTime time.Time, msg string)
	Sent(target string)
}

// MetricsManagerInterface provides comprehensive metrics management (for backward compatibility)
type MetricsManagerInterface interface {
	MetricsProvider
	MetricsSystemManager
	MetricsEventRecorder
	
	// Legacy methods - will be removed in future versions
	GetAllTargets() []string
	GetTargetHistory(target string, n int) []HistoryEntry
	GetAllTargetsRecentHistory(n int) map[string][]HistoryEntry
	SortBy(k Key, ascending bool) []MetricsReader
}