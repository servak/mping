package stats

import (
	"fmt"
	"time"
)

type Metrics struct {
	Total        int
	Successful   int
	Failed       int
	Loss         float64
	TotalRTT     time.Duration
	AverageRTT   time.Duration
	MinimumRTT   time.Duration
	MaximumRTT   time.Duration
	LastRTT      time.Duration
	LastFailTime time.Time
	LastSuccTime time.Time
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

func (m *Metrics) Fail(sentTime time.Time) {
	m.Failed++
	m.LastFailTime = sentTime
	m.loss()
}

func (m *Metrics) loss() {
	m.Loss = float64(m.Failed) / float64(m.Successful+m.Failed) * 100
}

func (m *Metrics) Sent() {
	m.Total++
}

// for debug
func (m *Metrics) Values() string {
	return fmt.Sprintf("%d %d %v", m.Total, m.Successful, m.Failed)
}
