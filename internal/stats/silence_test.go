package stats

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSilenceSet(t *testing.T) {
	s := NewSilenceSet("a")
	if !s.Contains("a") || s.Contains("b") {
		t.Fatal("initial contents wrong")
	}
	if !s.Toggle("b") || !s.Contains("b") {
		t.Error("Toggle should silence b")
	}
	if s.Toggle("b") || s.Contains("b") {
		t.Error("Toggle should un-silence b")
	}
	s.Add("c")
	s.Remove("a")
	if got := s.Names(); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("Names() = %v", got)
	}
}

func TestSilenceSetConcurrent(t *testing.T) {
	s := NewSilenceSet()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Toggle("x")
			s.Contains("x")
			s.Names()
		}()
	}
	wg.Wait()
}

func TestShouldBeep(t *testing.T) {
	tests := []struct {
		name string
		set  *SilenceSet
		key  string
		disp string
		want bool
	}{
		{"nil set beeps", nil, "k", "d", true},
		{"empty set beeps", NewSilenceSet(), "k", "d", true},
		{"silenced by key", NewSilenceSet("k"), "k", "d", false},
		{"silenced by display name", NewSilenceSet("d"), "k", "d", false},
		{"other target silenced", NewSilenceSet("x"), "k", "d", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBeep(tt.set, tt.key, tt.disp); got != tt.want {
				t.Errorf("shouldBeep = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerSilence(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{Silenced: []string{"quiet"}})
	sm, ok := mm.(interface {
		IsSilenced(string) bool
		ToggleSilence(string) bool
	})
	if !ok {
		t.Fatal("manager does not expose silence methods")
	}
	if !sm.IsSilenced("quiet") || sm.IsSilenced("loud") {
		t.Error("pre-silenced state wrong")
	}
	if !sm.ToggleSilence("loud") || !sm.IsSilenced("loud") {
		t.Error("ToggleSilence did not silence")
	}
	// Failures of a silenced target still update stats
	mm.(*metricsManager).Failed("quiet", time.Now(), "timeout")
	if got := mm.(*metricsManager).GetMetrics("quiet").GetFailed(); got != 1 {
		t.Errorf("GetFailed() = %d, want 1", got)
	}
}
