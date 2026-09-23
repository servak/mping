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

func TestMetricsJitter(t *testing.T) {
	m := NewMetrics("", 1)
	impl := m.(*metrics)
	now := time.Now()

	impl.Success(10*time.Millisecond, now)
	if got := m.GetJitter(); got != 0 {
		t.Errorf("jitter with a single sample should be 0, got %v", got)
	}

	// Samples: 10, 20, 30, 40ms -> population stddev = sqrt(125)ms ≈ 11.18ms
	impl.Success(20*time.Millisecond, now)
	impl.Success(30*time.Millisecond, now)
	impl.Success(40*time.Millisecond, now)
	// Failures must not affect jitter
	impl.Fail(now, "timeout")

	want := 11180339 * time.Nanosecond
	if diff := m.GetJitter() - want; diff < -time.Microsecond || diff > time.Microsecond {
		t.Errorf("GetJitter() = %v, want ~%v", m.GetJitter(), want)
	}

	impl.Reset()
	if got := m.GetJitter(); got != 0 {
		t.Errorf("jitter after reset should be 0, got %v", got)
	}
}
