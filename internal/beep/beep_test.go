package beep

import (
	"runtime"
	"testing"
	"time"
)

func TestNewBeeper(t *testing.T) {
	beeper := NewBeeper()

	if !beeper.IsEnabled() {
		t.Error("Expected beeper to be enabled by default")
	}

	if beeper.debounceTime != DefaultDebounceInterval {
		t.Errorf("Expected debounce time to be %v, got %v", DefaultDebounceInterval, beeper.debounceTime)
	}
}

func TestToggle(t *testing.T) {
	beeper := NewBeeper()

	initialState := beeper.IsEnabled()
	beeper.Toggle()

	if beeper.IsEnabled() == initialState {
		t.Error("Toggle did not change beeper state")
	}

	beeper.Toggle()
	if beeper.IsEnabled() != initialState {
		t.Error("Toggle did not restore original state")
	}
}

func TestDebounce(t *testing.T) {
	debounceInterval := 100 * time.Millisecond
	beeper := NewBeeper()
	beeper.debounceTime = debounceInterval

	// First beep should work
	beeper.Beep()
	time.Sleep(10 * time.Millisecond) // Wait for goroutine to execute

	firstBeepTime := beeper.GetLastBeepTime()
	if firstBeepTime.IsZero() {
		t.Error("Expected first beep to set lastBeepTime")
	}

	// Immediate second beep should be debounced
	beeper.Beep()
	time.Sleep(10 * time.Millisecond)

	if !beeper.GetLastBeepTime().Equal(firstBeepTime) {
		t.Error("Expected second beep to be debounced")
	}

	// Wait for debounce period and try again
	time.Sleep(debounceInterval)
	beeper.Beep()
	time.Sleep(10 * time.Millisecond)

	if beeper.GetLastBeepTime().Equal(firstBeepTime) {
		t.Error("Expected beep to work after debounce period")
	}
}

func TestBeepWhenDisabled(t *testing.T) {
	beeper := NewBeeper()
	beeper.Toggle() // disable

	// Beep should not update lastBeepTime when disabled
	beeper.Beep()
	time.Sleep(10 * time.Millisecond)

	if !beeper.GetLastBeepTime().IsZero() {
		t.Error("Expected lastBeepTime to remain zero when disabled")
	}
}

func TestConcurrentAccess(t *testing.T) {
	beeper := NewBeeper()
	done := make(chan bool)

	// Test concurrent beep calls
	for i := 0; i < 10; i++ {
		go func() {
			beeper.Beep()
			done <- true
		}()
	}

	// Test concurrent state changes
	for i := 0; i < 10; i++ {
		go func() {
			beeper.Toggle()
			_ = beeper.IsEnabled()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// If we get here without deadlock or panic, the test passes
}

type blockingWriter struct{ release chan struct{} }

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

func TestBeepDoesNotBlockWhenOutputBlocked(t *testing.T) {
	w := &blockingWriter{release: make(chan struct{})}
	defer close(w.release)
	b := NewBeeper()
	b.out = w
	b.debounceTime = 0
	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		b.Beep()
	}
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() { b.Toggle(); b.IsEnabled(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Toggle/IsEnabled blocked by a stuck stdout")
	}
	if n := runtime.NumGoroutine() - before; n > 5 {
		t.Errorf("goroutines piled up: +%d", n)
	}
}
