package stats

import (
	"math"
	"slices"
	"time"
)

func NewMetrics(name string, historySize int) Metrics {
	return newMetrics(name, historySize, 0)
}

func newMetrics(name string, historySize int, settle time.Duration) *metrics {
	return &metrics{
		Name:    name,
		history: NewTargetHistory(historySize),
		outage:  newOutageTracker(settle),
	}
}

func NewMetricsForTest(name string, historySize, total, success, failed int, loss float64, totalRTT, averageRTT, minimumRTT, maximumRTT, lastRTT time.Duration, lastSuccTime, lastFailTime time.Time, lastFailDetail string) Metrics {
	return &metrics{
		Name:           name,
		Total:          total,
		Successful:     success,
		Failed:         failed,
		Loss:           loss,
		TotalRTT:       totalRTT,
		AverageRTT:     averageRTT,
		MinimumRTT:     minimumRTT,
		MaximumRTT:     maximumRTT,
		LastRTT:        lastRTT,
		LastSuccTime:   lastSuccTime,
		LastFailTime:   lastFailTime,
		LastFailDetail: lastFailDetail,
		history:        NewTargetHistory(historySize),
		outage:         newOutageTracker(0),
	}
}

type metrics struct {
	Name           string
	Total          int
	Successful     int
	Failed         int
	Loss           float64
	TotalRTT       time.Duration
	AverageRTT     time.Duration
	MinimumRTT     time.Duration
	MaximumRTT     time.Duration
	LastRTT        time.Duration
	sumSquaredRTT  float64 // sum of rtt^2 in ns^2, used to derive jitter
	LastFailTime   time.Time
	LastSuccTime   time.Time
	LastFailDetail string
	history        *TargetHistory // 履歴情報
	outage         *outageTracker
}

func (m *metrics) Success(rtt time.Duration, sentTime time.Time) {
	m.Successful++
	m.LastSuccTime = sentTime
	m.LastRTT = rtt
	m.TotalRTT += rtt
	m.sumSquaredRTT += float64(rtt) * float64(rtt)
	m.AverageRTT = m.TotalRTT / time.Duration(m.Successful)
	if m.MinimumRTT == 0 || rtt < m.MinimumRTT {
		m.MinimumRTT = rtt
	}
	if rtt > m.MaximumRTT {
		m.MaximumRTT = rtt
	}
	m.loss()
}

func (m *metrics) Fail(sentTime time.Time, msg string) {
	m.Failed++
	m.LastFailTime = sentTime
	m.LastFailDetail = msg
	m.loss()
}

func (m *metrics) loss() {
	m.Loss = float64(m.Failed) / float64(m.Successful+m.Failed) * 100
}

func (m *metrics) Sent() {
	m.Total++
}

func (m *metrics) Reset() {
	m.Total = 0
	m.Successful = 0
	m.Failed = 0
	m.Loss = 0.0
	m.TotalRTT = time.Duration(0)
	m.AverageRTT = time.Duration(0)
	m.MinimumRTT = time.Duration(0)
	m.MaximumRTT = time.Duration(0)
	m.LastRTT = time.Duration(0)
	m.sumSquaredRTT = 0
	m.LastFailTime = time.Time{}
	m.LastSuccTime = time.Time{}
	m.LastFailDetail = ""
	if m.history != nil {
		m.history.Clear()
	}
	if m.outage != nil {
		m.outage.reset()
	}
}

// settledSnapshot applies outage results that have settled by now and returns
// a snapshot, so every read path sees outages up to date.
func (m *metrics) settledSnapshot(now time.Time) *metrics {
	if m.outage != nil {
		m.outage.flush(now)
	}
	return m.snapshot()
}

// snapshot returns a deep copy that can be read without holding the manager lock
func (m *metrics) snapshot() *metrics {
	c := *m
	if m.history != nil {
		c.history = m.history.clone()
	}
	if m.outage != nil {
		c.outage = m.outage.clone()
	}
	return &c
}

// Implementation of MetricsReader interface

func (m *metrics) GetName() string {
	return m.Name
}

func (m *metrics) GetTotal() int {
	return m.Total
}

func (m *metrics) GetSuccessful() int {
	return m.Successful
}

func (m *metrics) GetFailed() int {
	return m.Failed
}

func (m *metrics) GetLoss() float64 {
	return m.Loss
}

func (m *metrics) GetLastRTT() time.Duration {
	return m.LastRTT
}

func (m *metrics) GetAverageRTT() time.Duration {
	return m.AverageRTT
}

func (m *metrics) GetMinimumRTT() time.Duration {
	return m.MinimumRTT
}

func (m *metrics) GetMaximumRTT() time.Duration {
	return m.MaximumRTT
}

// GetJitter returns the standard deviation of successful RTTs
// (equivalent to "mdev" reported by iputils ping).
func (m *metrics) GetJitter() time.Duration {
	if m.Successful < 2 {
		return 0
	}
	n := float64(m.Successful)
	mean := float64(m.TotalRTT) / n
	variance := m.sumSquaredRTT/n - mean*mean
	if variance <= 0 {
		return 0
	}
	return time.Duration(math.Sqrt(variance))
}

func (m *metrics) GetLastSuccTime() time.Time {
	return m.LastSuccTime
}

func (m *metrics) GetLastFailTime() time.Time {
	return m.LastFailTime
}

func (m *metrics) GetLastFailDetail() string {
	return m.LastFailDetail
}

func (m *metrics) GetRecentHistory(n int) []HistoryEntry {
	if m.history == nil {
		return []HistoryEntry{}
	}
	return m.history.GetRecentEntries(n)
}

func (m *metrics) GetConsecutiveFailures() int {
	if m.history == nil {
		return 0
	}
	return m.history.GetConsecutiveFailures()
}

func (m *metrics) GetConsecutiveSuccesses() int {
	if m.history == nil {
		return 0
	}
	return m.history.GetConsecutiveSuccesses()
}

func (m *metrics) GetOutages() []Outage {
	if m.outage == nil {
		return []Outage{}
	}
	return m.outage.outages()
}

// GetLastOutage returns the ongoing outage, or else the most recent one
func (m *metrics) GetLastOutage() (Outage, bool) {
	if m.outage == nil {
		return Outage{}, false
	}
	return m.outage.last()
}

func (m *metrics) GetOutageSummary() OutageSummary {
	if m.outage == nil {
		return OutageSummary{}
	}
	return m.outage.summary(time.Now())
}

// GetRTTPercentiles returns RTT percentiles (0-100, nearest-rank method)
// over the successful probes kept in history.
func (m *metrics) GetRTTPercentiles(ps ...float64) []time.Duration {
	res := make([]time.Duration, len(ps))
	if m.history == nil {
		return res
	}
	var rtts []time.Duration
	for _, e := range m.history.GetRecentEntries(m.history.count) {
		if e.Success {
			rtts = append(rtts, e.RTT)
		}
	}
	if len(rtts) == 0 {
		return res
	}
	slices.Sort(rtts)
	for i, p := range ps {
		rank := int(math.Ceil(p / 100 * float64(len(rtts))))
		rank = min(max(rank, 1), len(rtts))
		res[i] = rtts[rank-1]
	}
	return res
}

func (m *metrics) GetSuccessRateInPeriod(duration time.Duration) float64 {
	if m.history == nil {
		return 0.0
	}
	return m.history.GetSuccessRateInPeriod(duration)
}
