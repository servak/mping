package beep

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	// DefaultDebounceInterval is the default interval for debouncing beep sounds
	DefaultDebounceInterval = 500 * time.Millisecond
)

// Beeper manages beep sound playback with debouncing. The zero value is
// ready to use (enabled, writing to os.Stdout, no debounce); use NewBeeper
// for the default debounce interval. Call Close to stop the worker goroutine.
type Beeper struct {
	disabled     bool // inverted so that the zero value is enabled
	debounceTime time.Duration
	lastBeepTime time.Time
	mu           sync.Mutex
	out          io.Writer
	queue        chan struct{}
	stop         chan struct{}
	done         chan struct{}
	initOnce     sync.Once
	closeOnce    sync.Once
}

// NewBeeper creates a new Beeper instance
func NewBeeper() *Beeper {
	return &Beeper{
		debounceTime: DefaultDebounceInterval,
		out:          os.Stdout,
	}
}

// init lazily prepares the queue and starts the worker, so that a zero-value
// Beeper works too.
func (b *Beeper) init() {
	b.initOnce.Do(func() {
		b.mu.Lock()
		if b.out == nil {
			b.out = os.Stdout
		}
		b.mu.Unlock()
		b.queue = make(chan struct{}, 1)
		b.stop = make(chan struct{})
		b.done = make(chan struct{})
		go b.worker()
	})
}

// Beep requests a beep sound. It never blocks: requests are handed to a
// single worker through a one-slot queue and extra requests are dropped.
func (b *Beeper) Beep() {
	b.init()
	select {
	case b.queue <- struct{}{}:
	default: // a beep is already pending
	}
}

// closeWait bounds how long Close waits for the worker to exit.
const closeWait = 200 * time.Millisecond

// Close stops the worker goroutine and waits (up to closeWait) for it to
// exit. It is idempotent; Beep after Close is a harmless no-op.
func (b *Beeper) Close() {
	b.init()
	b.closeOnce.Do(func() { close(b.stop) })
	// Wait briefly for the worker; if it is stuck writing to a blocked
	// terminal, don't hang the caller (e.g. app shutdown) on it.
	select {
	case <-b.done:
	case <-time.After(closeWait):
	}
}

func (b *Beeper) worker() {
	defer close(b.done)
	for {
		select {
		case <-b.stop:
			return
		case <-b.queue:
		}
		b.mu.Lock()
		now := time.Now()
		play := !b.disabled && now.Sub(b.lastBeepTime) >= b.debounceTime
		if play {
			b.lastBeepTime = now
		}
		out := b.out
		b.mu.Unlock()
		if !play {
			continue
		}
		// Write outside the lock so a blocked stdout cannot freeze Toggle
		if err := playBeep(out); err != nil {
			// Log error but don't propagate
			fmt.Fprintf(os.Stderr, "beep: failed to play sound: %v\n", err)
		}
	}
}

// playBeep outputs the system bell character
func playBeep(out io.Writer) error {
	_, err := fmt.Fprint(out, "\a")
	return err
}

// Toggle switches beep sound on/off and returns the new state
func (b *Beeper) Toggle() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.disabled = !b.disabled
	return !b.disabled
}

// IsEnabled returns current beep sound state
func (b *Beeper) IsEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.disabled
}

// GetLastBeepTime returns the last beep time (for testing)
func (b *Beeper) GetLastBeepTime() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastBeepTime
}
