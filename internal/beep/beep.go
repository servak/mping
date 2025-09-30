package beep

import (
	"fmt"
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
}

// NewBeeper creates a new Beeper instance
func NewBeeper() *Beeper {
	return &Beeper{
		enabled:      true, // Default is ON
		debounceTime: DefaultDebounceInterval,
	}
}

// NewBeeperWithDebounce creates a new Beeper with custom debounce interval
func NewBeeperWithDebounce(debounceInterval time.Duration) *Beeper {
	return &Beeper{
		enabled:      true,
		debounceTime: debounceInterval,
	}
}

// Beep plays a beep sound if enabled and debounce time has passed
// This function is non-blocking and thread-safe
func (b *Beeper) Beep() {
	go func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		// Skip if disabled
		if !b.enabled {
			return
		}

		// Check debounce time
		now := time.Now()
		if now.Sub(b.lastBeepTime) < b.debounceTime {
			return // Skip if within debounce period
		}

		// Play beep sound
		if err := b.playBeep(); err != nil {
			// Log error but don't propagate
			// This ensures beep failures don't affect main processing
			fmt.Fprintf(os.Stderr, "beep: failed to play sound: %v\n", err)
		}

		b.lastBeepTime = now
	}()
}

// playBeep outputs the system bell character
func (b *Beeper) playBeep() error {
	_, err := fmt.Print("\a")
	return err
}

// Enable turns on beep sound
func (b *Beeper) Enable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = true
}

// Disable turns off beep sound
func (b *Beeper) Disable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = false
}

// Toggle switches beep sound on/off
func (b *Beeper) Toggle() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = !b.enabled
}

// IsEnabled returns current beep sound state
func (b *Beeper) IsEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// SetDebounceInterval sets the debounce interval
func (b *Beeper) SetDebounceInterval(interval time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.debounceTime = interval
}
