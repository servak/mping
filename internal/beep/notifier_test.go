package beep

import (
	"sync"
	"sync/atomic"
	"testing"
)

// countingNotifier returns a Notifier whose bell only increments a counter
func countingNotifier(silenced ...string) (*Notifier, *atomic.Int32) {
	n := NewNotifier()
	for _, name := range silenced {
		n.ToggleSilence(name)
	}
	var rang atomic.Int32
	n.beep = func() { rang.Add(1) }
	return n, &rang
}

func TestNotifierSilence(t *testing.T) {
	n, _ := countingNotifier("quiet")
	if !n.IsSilenced("quiet") || n.IsSilenced("loud") {
		t.Error("initial silenced state wrong")
	}
	if !n.ToggleSilence("loud") || !n.IsSilenced("loud") {
		t.Error("ToggleSilence did not silence")
	}
	if n.ToggleSilence("loud") || n.IsSilenced("loud") {
		t.Error("ToggleSilence did not un-silence")
	}
}

func TestNotifierOnFailureSkipsSilenced(t *testing.T) {
	n, rang := countingNotifier("quiet")
	n.OnFailure("quiet")
	if got := rang.Load(); got != 0 {
		t.Errorf("silenced target rang %d times, want 0", got)
	}
	n.OnFailure("loud")
	if got := rang.Load(); got != 1 {
		t.Errorf("non-silenced target rang %d times, want 1", got)
	}
}

func TestNotifierToggle(t *testing.T) {
	n, _ := countingNotifier()
	if !n.Enabled() {
		t.Fatal("beep should be enabled by default")
	}
	if n.Toggle() || n.Enabled() {
		t.Error("Toggle did not disable the beep")
	}
	if !n.Toggle() || !n.Enabled() {
		t.Error("Toggle did not re-enable the beep")
	}
}

func TestNotifierConcurrent(t *testing.T) {
	n, _ := countingNotifier()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.ToggleSilence("x")
			n.IsSilenced("x")
			n.OnFailure("x")
			n.Toggle()
			n.Enabled()
		}()
	}
	wg.Wait()
}
