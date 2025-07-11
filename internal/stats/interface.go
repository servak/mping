package stats

import (
	"time"

	"github.com/servak/mping/internal/prober"
)

// Metrics provides basic statistics for display and sorting
type Metrics interface {
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

	GetRecentHistory(n int) []HistoryEntry
	GetConsecutiveFailures() int
	GetConsecutiveSuccesses() int
	GetSuccessRateInPeriod(duration time.Duration) float64
}

// MetricsProvider provides external API for metrics access
type MetricsProvider interface {
	SortBy(k Key, ascending bool) []Metrics
}

// MetricsSystemManager provides system-level operations
type MetricsSystemManager interface {
	ResetAllMetrics()
}

// MetricsEventRecorder handles internal event recording
type MetricsEventRecorder interface {
	Register(target, name string)
	Subscribe(<-chan *prober.Event)
}

// MetricsManager provides comprehensive metrics management (for backward compatibility)
type MetricsManager interface {
	MetricsProvider
	MetricsSystemManager
	MetricsEventRecorder
}
