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

// Beeper manages beep sound playback with debouncing
type Beeper struct {
	enabled      bool
	debounceTime time.Duration
	lastBeepTime time.Time
	mu           sync.Mutex
	out          io.Writer
	queue        chan struct{}
	startOnce    sync.Once
}

// NewBeeper creates a new Beeper instance
func NewBeeper() *Beeper {
	return &Beeper{
		enabled:      true, // Default is ON
		debounceTime: DefaultDebounceInterval,
		out:          os.Stdout,
		queue:        make(chan struct{}, 1),
	}
}

// Beep requests a beep sound. It never blocks: requests are handed to a
// single worker through a one-slot queue and extra requests are dropped.
func (b *Beeper) Beep() {
	b.startOnce.Do(func() { go b.worker() })
	select {
	case b.queue <- struct{}{}:
	default: // a beep is already pending
	}
}

func (b *Beeper) worker() {
	for range b.queue {
		b.mu.Lock()
		now := time.Now()
		play := b.enabled && now.Sub(b.lastBeepTime) >= b.debounceTime
		if play {
			b.lastBeepTime = now
		}
		b.mu.Unlock()
		if !play {
			continue
		}
		// Write outside the lock so a blocked stdout cannot freeze Toggle
		if err := b.playBeep(); err != nil {
			// Log error but don't propagate
			fmt.Fprintf(os.Stderr, "beep: failed to play sound: %v\n", err)
		}
	}
}

// playBeep outputs the system bell character
func (b *Beeper) playBeep() error {
	_, err := fmt.Fprint(b.out, "\a")
	return err
}

// Toggle switches beep sound on/off and returns the new state
func (b *Beeper) Toggle() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = !b.enabled
	return b.enabled
}

// IsEnabled returns current beep sound state
func (b *Beeper) IsEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// GetLastBeepTime returns the last beep time (for testing)
func (b *Beeper) GetLastBeepTime() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastBeepTime
}
