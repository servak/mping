package stats

import (
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := NewMetrics("", 1)

	now := time.Now()

	// メソッドとして実装されているSentを呼び出す
	metricsImpl := m.(*metrics)
	metricsImpl.Sent()
	metricsImpl.Success(100*time.Millisecond, now)

	if m.GetTotal() != 1 || m.GetSuccessful() != 1 || m.GetFailed() != 0 {
		t.Errorf("Invalid values after first success: Total = %d, Successful = %d, Failed = %d", m.GetTotal(), m.GetSuccessful(), m.GetFailed())
	}

	if m.GetAverageRTT() != 100*time.Millisecond || m.GetLastRTT() != 100*time.Millisecond || m.GetLastSuccTime() != now {
		t.Errorf("Invalid RTT values after first success: AverageRTT = %v, LastRTT = %v, LastSuccTime = %v", m.GetAverageRTT(), m.GetLastRTT(), m.GetLastSuccTime())
	}

	metricsImpl.Sent()
	metricsImpl.Fail(now, "timeout")

	if m.GetTotal() != 2 || m.GetSuccessful() != 1 || m.GetFailed() != 1 {
		t.Errorf("Invalid values after first failure: Total = %d, Successful = %d, Failed = %d", m.GetTotal(), m.GetSuccessful(), m.GetFailed())
	}

	if m.GetLastFailTime() != now {
		t.Errorf("Invalid fail time after first failure: LastFailTime = %v", m.GetLastFailTime())
	}

	metricsImpl.Sent()
	metricsImpl.Success(50*time.Millisecond, now)

	if m.GetAverageRTT() != 75*time.Millisecond || m.GetLastRTT() != 50*time.Millisecond || m.GetLastSuccTime() != now {
		t.Errorf("Invalid RTT values after second success: AverageRTT = %v, LastRTT = %v, LastSuccTime = %v", m.GetAverageRTT(), m.GetLastRTT(), m.GetLastSuccTime())
	}

	if m.GetMinimumRTT() != 50*time.Millisecond || m.GetMaximumRTT() != 100*time.Millisecond {
		t.Errorf("Invalid min/max RTT values after second success: MinimumRTT = %v, MaximumRTT = %v", m.GetMinimumRTT(), m.GetMaximumRTT())
	}

	if m.GetLoss() != 33.33333333333333 {
		t.Errorf("Invalid loss calculation: Loss = %f", m.GetLoss())
	}
}
