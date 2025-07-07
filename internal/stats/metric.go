package stats

import (
	"time"
)

type Metrics struct {
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

func (m *Metrics) Success(rtt time.Duration, sentTime time.Time) {
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

func (m *Metrics) Fail(sentTime time.Time, msg string) {
	m.Failed++
	m.LastFailTime = sentTime
	m.LastFailDetail = msg
	m.loss()
}

func (m *Metrics) loss() {
	m.Loss = float64(m.Failed) / float64(m.Successful+m.Failed) * 100
}

func (m *Metrics) Sent() {
	m.Total++
}

func (m *Metrics) Reset() {
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

// MetricsReader インターフェースの実装

func (m *Metrics) GetName() string {
	return m.Name
}

func (m *Metrics) GetTotal() int {
	return m.Total
}

func (m *Metrics) GetSuccessful() int {
	return m.Successful
}

func (m *Metrics) GetFailed() int {
	return m.Failed
}

func (m *Metrics) GetLoss() float64 {
	return m.Loss
}

func (m *Metrics) GetLastRTT() time.Duration {
	return m.LastRTT
}

func (m *Metrics) GetAverageRTT() time.Duration {
	return m.AverageRTT
}

func (m *Metrics) GetMinimumRTT() time.Duration {
	return m.MinimumRTT
}

func (m *Metrics) GetMaximumRTT() time.Duration {
	return m.MaximumRTT
}

func (m *Metrics) GetLastSuccTime() time.Time {
	return m.LastSuccTime
}

func (m *Metrics) GetLastFailTime() time.Time {
	return m.LastFailTime
}

func (m *Metrics) GetLastFailDetail() string {
	return m.LastFailDetail
}

func (m *Metrics) GetRecentHistory(n int) []HistoryEntry {
	if m.history == nil {
		return []HistoryEntry{}
	}
	return m.history.GetRecentEntries(n)
}

func (m *Metrics) GetHistorySince(since time.Time) []HistoryEntry {
	if m.history == nil {
		return []HistoryEntry{}
	}
	return m.history.GetEntriesSince(since)
}

func (m *Metrics) GetConsecutiveFailures() int {
	if m.history == nil {
		return 0
	}
	return m.history.GetConsecutiveFailures()
}

func (m *Metrics) GetConsecutiveSuccesses() int {
	if m.history == nil {
		return 0
	}
	return m.history.GetConsecutiveSuccesses()
}

func (m *Metrics) GetSuccessRateInPeriod(duration time.Duration) float64 {
	if m.history == nil {
		return 0.0
	}
	return m.history.GetSuccessRateInPeriod(duration)
}
