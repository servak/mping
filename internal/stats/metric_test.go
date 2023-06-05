package stats

import (
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := &Metrics{}

	now := time.Now()

	m.Sent()
	m.Success(100*time.Millisecond, now)

	if m.Total != 1 || m.Successful != 1 || m.Failed != 0 {
		t.Errorf("Invalid values after first success: Total = %d, Successful = %d, Failed = %d", m.Total, m.Successful, m.Failed)
	}

	if m.AverageRTT != 100*time.Millisecond || m.LastRTT != 100*time.Millisecond || m.LastSuccTime != now {
		t.Errorf("Invalid RTT values after first success: AverageRTT = %v, LastRTT = %v, LastSuccTime = %v", m.AverageRTT, m.LastRTT, m.LastSuccTime)
	}

	m.Sent()
	m.Fail(now)

	if m.Total != 2 || m.Successful != 1 || m.Failed != 1 {
		t.Errorf("Invalid values after first failure: Total = %d, Successful = %d, Failed = %d", m.Total, m.Successful, m.Failed)
	}

	if m.LastFailTime != now {
		t.Errorf("Invalid fail time after first failure: LastFailTime = %v", m.LastFailTime)
	}

	m.Sent()
	m.Success(50*time.Millisecond, now)

	if m.AverageRTT != 75*time.Millisecond || m.LastRTT != 50*time.Millisecond || m.LastSuccTime != now {
		t.Errorf("Invalid RTT values after second success: AverageRTT = %v, LastRTT = %v, LastSuccTime = %v", m.AverageRTT, m.LastRTT, m.LastSuccTime)
	}

	if m.MinimumRTT != 50*time.Millisecond || m.MaximumRTT != 100*time.Millisecond {
		t.Errorf("Invalid min/max RTT values after second success: MinimumRTT = %v, MaximumRTT = %v", m.MinimumRTT, m.MaximumRTT)
	}

	if m.Loss != 33.33333333333333 {
		t.Errorf("Invalid loss calculation: Loss = %f", m.Loss)
	}
}
