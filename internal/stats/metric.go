package stats

import (
	"time"
)

func NewMetrics(name string, historySize int) Metrics {
	return &metrics{
		Name:    name,
		history: NewTargetHistory(historySize),
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
	LastFailTime   time.Time
	LastSuccTime   time.Time
	LastFailDetail string
	history        *TargetHistory // 履歴情報
}

func (m *metrics) Success(rtt time.Duration, sentTime time.Time) {
	m.Successful++
	m.LastSuccTime = sentTime
	m.LastRTT = rtt
	m.TotalRTT += rtt
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
	m.LastFailTime = time.Time{}
	m.LastSuccTime = time.Time{}
	m.LastFailDetail = ""
	if m.history != nil {
		m.history.Clear()
	}
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

func (m *metrics) GetSuccessRateInPeriod(duration time.Duration) float64 {
	if m.history == nil {
		return 0.0
	}
	return m.history.GetSuccessRateInPeriod(duration)
}
