package stats

import (
	"sort"
	"sync"
	"time"

	"github.com/servak/mping/internal/beep"
	"github.com/servak/mping/internal/prober"
)

const (
	DefaultHistorySize = 100 // Default number of history entries to keep
)

type metricsManager struct {
	metrics     map[string]*metrics
	historySize int // Number of history entries to keep
	settle      time.Duration
	beeper      *beep.Beeper
	mu          sync.Mutex
}

// Options configures a MetricsManager
type Options struct {
	HistorySize int
	// SettleTime is how long results are buffered before outage detection so
	// that out-of-order results can be applied in send order. It should cover
	// the latest a result can be reported: timeout plus one interval (ICMP
	// timeouts are checked on the interval ticker).
	SettleTime time.Duration
	// DisableBeep turns off the failure beep, e.g. for non-interactive runs
	DisableBeep bool
}

// SettleTimeFor returns the recommended SettleTime for the probe timing
func SettleTimeFor(interval, timeout time.Duration) time.Duration {
	return interval + timeout + 100*time.Millisecond
}

// Create a new MetricsManager
func NewMetricsManager() MetricsManager {
	return NewMetricsManagerWithOptions(Options{})
}

// NewMetricsManagerWithOptions creates a MetricsManager with custom options
func NewMetricsManagerWithOptions(opts Options) MetricsManager {
	if opts.HistorySize <= 0 {
		opts.HistorySize = DefaultHistorySize
	}
	mm := &metricsManager{
		metrics:     make(map[string]*metrics),
		historySize: opts.HistorySize,
		settle:      opts.SettleTime,
	}
	if !opts.DisableBeep {
		mm.beeper = beep.NewBeeper()
	}
	return mm
}

func (mm *metricsManager) newMetrics(name string) *metrics {
	return newMetrics(name, mm.historySize, mm.settle)
}

func (mm *metricsManager) Register(target, name string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	v, ok := mm.metrics[target]
	if ok && v.Name != target {
		return
	}
	mm.metrics[target] = mm.newMetrics(name)
}

// 指定されたホストのMetricsを取得（内部用）
func (mm *metricsManager) getMetrics(host string) *metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		m = mm.newMetrics(host)
		// Register the new metrics for the host
		mm.metrics[host] = m
	}
	return m
}

func (mm *metricsManager) GetMetrics(host string) Metrics {
	m := mm.getMetrics(host)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return m.settledSnapshot(time.Now())
}

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
	m.outage.add(sentTime, true, "", time.Now())
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
	m.outage.add(sentTime, false, msg, time.Now())
	mm.mu.Unlock()

	// Play beep sound on failure (non-blocking)
	if mm.beeper != nil {
		mm.beeper.Beep()
	}
}

func (mm *metricsManager) Sent(host string) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	m.Sent()
	mm.mu.Unlock()
}

func (mm *metricsManager) Subscribe(res <-chan *prober.Event) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Once the channel is closed no more results can arrive, so every
		// buffered result can be applied to outage detection.
		defer mm.flushOutages()
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
	return done
}

// autoRegister automatically registers target if not already registered
func (mm *metricsManager) autoRegister(key, displayName string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if _, exists := mm.metrics[key]; !exists {
		mm.metrics[key] = mm.newMetrics(displayName)
	}
}

func (mm *metricsManager) flushOutages() {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	for _, m := range mm.metrics {
		m.outage.flushAll()
	}
}

// SortBy sorts metrics by specified key and returns Metrics slice.
// The returned values are snapshots, safe to read while probing continues.
func (mm *metricsManager) SortBy(k Key, ascending bool) []Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	now := time.Now()
	var res []Metrics
	for _, m := range mm.metrics {
		res = append(res, m.settledSnapshot(now))
	}

	if k != Host {
		sort.SliceStable(res, func(i, j int) bool {
			return res[i].GetName() < res[j].GetName()
		})
	}
	// less must be a strict ordering: descending order swaps the operands
	// instead of negating the result, which would make equal elements
	// "less" than each other and reorder them on every call.
	less := func(mi, mj Metrics) bool {
		switch k {
		case Host:
			return mi.GetName() < mj.GetName()
		case Sent:
			return mi.GetTotal() < mj.GetTotal()
		case Success:
			return mi.GetSuccessful() < mj.GetSuccessful()
		case Loss:
			return mi.GetLoss() < mj.GetLoss()
		case Fail:
			return mi.GetFailed() < mj.GetFailed()
		case Last, Avg, Best, Worst:
			return rttFor(k, mi) < rttFor(k, mj)
		case LastSuccTime:
			return mi.GetLastSuccTime().Before(mj.GetLastSuccTime())
		case LastFailTime:
			return mi.GetLastFailTime().Before(mj.GetLastFailTime())
		}
		return false
	}
	sort.SliceStable(res, func(i, j int) bool {
		// Unmeasured RTTs (zero) always go last, whatever the direction
		if zi, zj := rttFor(k, res[i]) == 0, rttFor(k, res[j]) == 0; zi != zj {
			return zj
		}
		if ascending {
			return less(res[i], res[j])
		}
		return less(res[j], res[i])
	})
	return res
}

// GetMetricsAsReader retrieves as Metrics interface
func (mm *metricsManager) GetMetricsAsReader(target string) Metrics {
	return mm.GetMetrics(target)
}

// ToggleBeep toggles beep sound on/off
func (mm *metricsManager) ToggleBeep() {
	if mm.beeper != nil {
		mm.beeper.Toggle()
	}
}

// IsBeepEnabled returns current beep sound state
func (mm *metricsManager) IsBeepEnabled() bool {
	if mm.beeper != nil {
		return mm.beeper.IsEnabled()
	}
	return false
}

// rttFor returns the RTT a sort key refers to, or -1 for non-RTT keys
func rttFor(k Key, m Metrics) time.Duration {
	switch k {
	case Last:
		return m.GetLastRTT()
	case Avg:
		return m.GetAverageRTT()
	case Best:
		return m.GetMinimumRTT()
	case Worst:
		return m.GetMaximumRTT()
	}
	return -1
}
