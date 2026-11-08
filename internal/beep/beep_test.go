package beep

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNewBeeper(t *testing.T) {
	beeper := NewBeeper()
	defer beeper.Close()

	if !beeper.IsEnabled() {
		t.Error("Expected beeper to be enabled by default")
	}

	if beeper.debounceTime != DefaultDebounceInterval {
		t.Errorf("Expected debounce time to be %v, got %v", DefaultDebounceInterval, beeper.debounceTime)
	}
}

func TestToggle(t *testing.T) {
	beeper := NewBeeper()
	defer beeper.Close()

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
	defer beeper.Close()
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
	defer beeper.Close()
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
	defer beeper.Close()
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
	b := NewBeeper()
	// Close waits for the worker, so release the writer first (LIFO defers)
	defer b.Close()
	defer close(w.release)
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

func TestCloseIdempotentAndStopsWorker(t *testing.T) {
	before := runtime.NumGoroutine()
	b := NewBeeper()
	b.Beep()
	b.Close()
	b.Close() // second call must not panic or block
	b.Beep()  // no-op after Close
	time.Sleep(20 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("worker goroutine leaked: %d > %d", n, before)
	}
}

func TestCloseWithoutBeep(t *testing.T) {
	b := NewBeeper()
	b.Close()
	b.Close()
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestZeroValueBeeperWorks(t *testing.T) {
	var b Beeper
	defer b.Close()
	if !b.IsEnabled() {
		t.Error("zero value should be enabled")
	}
	b.Beep() // must not panic with nil out; writes to os.Stdout
	time.Sleep(20 * time.Millisecond)
	if b.GetLastBeepTime().IsZero() {
		t.Error("zero-value Beeper dropped the beep")
	}
}

func TestZeroValueBeeperWritesToOut(t *testing.T) {
	buf := &syncBuf{}
	b := &Beeper{out: buf}
	defer b.Close()
	b.Beep()
	time.Sleep(20 * time.Millisecond)
	if buf.String() != "\a" {
		t.Errorf("out = %q, want bell", buf.String())
	}
}
