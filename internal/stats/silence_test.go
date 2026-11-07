package stats

import (
	"sync"
	"testing"
	"time"
)

func TestManagerSilence(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{DisableBeep: true, Silenced: []string{"quiet"}})
	if !mm.IsSilenced("quiet") || mm.IsSilenced("loud") {
		t.Error("pre-silenced state wrong")
	}
	if !mm.ToggleSilence("loud") || !mm.IsSilenced("loud") {
		t.Error("ToggleSilence did not silence")
	}
	if mm.ToggleSilence("loud") || mm.IsSilenced("loud") {
		t.Error("ToggleSilence did not un-silence")
	}
}

func TestSilencedFailureStillCounted(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{DisableBeep: true, Silenced: []string{"quiet"}})
	mm.(*metricsManager).autoRegister("quiet", "quiet")
	mm.(*metricsManager).Failed("quiet", time.Now(), "timeout")
	if got := mm.(*metricsManager).GetMetrics("quiet").GetFailed(); got != 1 {
		t.Errorf("GetFailed() = %d, want 1", got)
	}
}

func TestSilenceConcurrent(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{DisableBeep: true})
	mm.(*metricsManager).autoRegister("x", "x")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mm.ToggleSilence("x")
			mm.IsSilenced("x")
			mm.(*metricsManager).Failed("x", time.Now(), "timeout")
		}()
	}
	wg.Wait()
}
