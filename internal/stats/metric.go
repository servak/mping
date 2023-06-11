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
}
