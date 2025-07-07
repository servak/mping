package stats

import (
	"time"
	
	"github.com/servak/mping/internal/prober"
)

// Interface for reading metrics by target
type MetricsReader interface {
	// Basic statistics
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

	// History information
	GetRecentHistory(n int) []HistoryEntry
	GetHistorySince(since time.Time) []HistoryEntry
	GetConsecutiveFailures() int
	GetConsecutiveSuccesses() int
	GetSuccessRateInPeriod(duration time.Duration) float64
}

// Interface for overall metrics management
type MetricsManagerInterface interface {
	// Basic operations
	GetMetrics(target string) MetricsReader
	GetAllTargets() []string
	ResetAllMetrics()

	// Statistics registration
	Success(target string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails)
	Failed(target string, sentTime time.Time, msg string)
	Sent(target string)

	// History functions
	GetTargetHistory(target string, n int) []HistoryEntry
	GetAllTargetsRecentHistory(n int) map[string][]HistoryEntry

	// Sort functions
	SortBy(k Key, ascending bool) []MetricsReader
}

// Interface dedicated to history management
type HistoryManagerInterface interface {
	AddSuccessEntry(target string, timestamp time.Time, rtt time.Duration, details *prober.ProbeDetails)
	AddFailureEntry(target string, timestamp time.Time, error string)
	GetHistory(target string, n int) []HistoryEntry
	GetHistorySince(target string, since time.Time) []HistoryEntry
	ClearHistory(target string)
	ClearAllHistory()
}