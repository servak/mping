package stats

import (
	"sort"
	"sync"
	"time"

	"github.com/servak/mping/internal/prober"
)

const (
	DefaultHistorySize = 100 // Default number of history entries to keep
)

type metricsManager struct {
	metrics     map[string]*Metrics
	historySize int // Number of history entries to keep
	mu          sync.Mutex
}

// Create a new MetricsManager
func NewMetricsManager() MetricsManager {
	return NewMetricsManagerWithHistorySize(DefaultHistorySize)
}

// Create MetricsManager with specified history size
func NewMetricsManagerWithHistorySize(historySize int) MetricsManager {
	return &metricsManager{
		metrics:     make(map[string]*Metrics),
		historySize: historySize,
	}
}

func (mm *metricsManager) Register(target, name string) {
	v, ok := mm.metrics[target]
	if ok && v.Name != target {
		return
	}
	mm.metrics[target] = &Metrics{
		Name:    name,
		history: NewTargetHistory(mm.historySize),
	}
}

// 指定されたホストのMetricsを取得（内部用）
func (mm *metricsManager) getMetrics(host string) *Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		m = &Metrics{
			Name:    host,
			history: NewTargetHistory(mm.historySize),
		}
		mm.metrics[host] = m
	}
	return m
}

// 指定されたホストのMetricsを取得（外部用）
func (mm *metricsManager) GetMetrics(host string) MetricsReader {
	return mm.getMetrics(host)
}

// 全てのMetricsをリセット
func (mm *metricsManager) ResetAllMetrics() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for _, m := range mm.metrics {
		m.Reset()
	}
}

// Register success for host
func (mm *metricsManager) Success(host string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails) {
	mm.SuccessWithDetails(host, rtt, sentTime, details)
}

// Register success for host with detailed information
func (mm *metricsManager) SuccessWithDetails(host string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	m.Success(rtt, sentTime)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       rtt,
			Success:   true,
			Details:   details,
		})
	}
	mm.mu.Unlock()
}

// Register failure for host
func (mm *metricsManager) Failed(host string, sentTime time.Time, msg string) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	m.Fail(sentTime, msg)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       0,
			Success:   false,
			Error:     msg,
		})
	}
	mm.mu.Unlock()
}

func (mm *metricsManager) Sent(host string) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	m.Sent()
	mm.mu.Unlock()
}

func (mm *metricsManager) Subscribe(res <-chan *prober.Event) {
	go func() {
		for r := range res {
			switch r.Result {
			case prober.REGISTER:
				mm.autoRegister(r.Key, r.DisplayName)
			case prober.SENT:
				mm.Sent(r.Key)
			case prober.SUCCESS:
				mm.SuccessWithDetails(r.Key, r.Rtt, r.SentTime, r.Details)
			case prober.TIMEOUT:
				mm.Failed(r.Key, r.SentTime, r.Message)
			case prober.FAILED:
				mm.Failed(r.Key, r.SentTime, r.Message)
			}
		}
	}()
}

// autoRegister automatically registers target if not already registered
func (mm *metricsManager) autoRegister(key, displayName string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if _, exists := mm.metrics[key]; !exists {
		mm.metrics[key] = &Metrics{
			Name:    displayName,
			history: NewTargetHistory(mm.historySize),
		}
	}
}

// SortBy sorts metrics by specified key and returns MetricsReader slice
func (mm *metricsManager) SortBy(k Key, ascending bool) []MetricsReader {
	mm.mu.Lock()
	var res []MetricsReader
	for _, m := range mm.metrics {
		res = append(res, m)
	}
	mm.mu.Unlock()

	if k != Host {
		sort.SliceStable(res, func(i, j int) bool {
			return res[i].GetName() < res[j].GetName()
		})
	}
	sort.SliceStable(res, func(i, j int) bool {
		mi := res[i]
		mj := res[j]
		var result bool
		switch k {
		case Host:
			result = mi.GetName() < mj.GetName()
		case Sent:
			result = mi.GetTotal() < mj.GetTotal()
		case Success:
			result = mi.GetSuccessful() < mj.GetSuccessful()
		case Loss:
			result = mi.GetLoss() < mj.GetLoss()
		case Fail:
			result = mi.GetFailed() < mj.GetFailed()
		case Last:
			result = rejectLessAscending(mi.GetLastRTT(), mj.GetLastRTT())
		case Avg:
			result = rejectLessAscending(mi.GetAverageRTT(), mj.GetAverageRTT())
		case Best:
			result = rejectLessAscending(mi.GetMinimumRTT(), mj.GetMinimumRTT())
		case Worst:
			result = rejectLessAscending(mi.GetMaximumRTT(), mj.GetMaximumRTT())
		case LastSuccTime:
			result = mi.GetLastSuccTime().Before(mj.GetLastSuccTime())
		case LastFailTime:
			result = mi.GetLastFailTime().Before(mj.GetLastFailTime())
		default:
			return false
		}

		if ascending {
			return result
		} else {
			return !result
		}
	})
	return res
}

// GetMetricsAsReader retrieves as MetricsReader interface
func (mm *metricsManager) GetMetricsAsReader(target string) MetricsReader {
	return mm.getMetrics(target)
}

// rejectLessAscending is RTT comparison function for ascending sort
// Zero values (unmeasured) are always placed at the end
func rejectLessAscending(i, j time.Duration) bool {
	if i == 0 {
		return false // If i is 0, put j first
	}
	if j == 0 {
		return true // If j is 0, put i first
	}
	return i < j // If both are non-zero, put the smaller one first
}
