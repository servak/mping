package beep

import "sync"

// Notifier decides whether a probe failure should ring the terminal bell.
// It combines the global beep switch (the Beeper) with a per-target silence
// list keyed by display name. Silencing only affects the bell: statistics
// keep counting failures as usual.
type Notifier struct {
	beeper   *Beeper
	beep     func() // rings the bell; replaced in tests
	mu       sync.Mutex
	silenced map[string]struct{}
}

// NewNotifier returns a Notifier with the beep enabled and no target silenced.
// Targets are silenced at runtime with ToggleSilence (the TUI "m" key).
func NewNotifier() *Notifier {
	b := NewBeeper()
	return &Notifier{
		beeper:   b,
		beep:     b.Beep,
		silenced: make(map[string]struct{}),
	}
}

// OnFailure rings the bell for a failure of the named target (display name)
// unless the target is silenced. It never blocks; the Beeper debounces and
// honours the global switch.
func (n *Notifier) OnFailure(name string) {
	if n.IsSilenced(name) {
		return
	}
	n.beep()
}

// Toggle switches the global beep on/off and returns the new state
func (n *Notifier) Toggle() bool {
	return n.beeper.Toggle()
}

// Enabled reports whether the global beep is on
func (n *Notifier) Enabled() bool {
	return n.beeper.IsEnabled()
}

// ToggleSilence mutes/unmutes the bell for one target (display name) and
// returns the new state
func (n *Notifier) ToggleSilence(name string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.silenced[name]; ok {
		delete(n.silenced, name)
		return false
	}
	n.silenced[name] = struct{}{}
	return true
}

// IsSilenced reports whether the target (display name) is silenced
func (n *Notifier) IsSilenced(name string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.silenced[name]
	return ok
}
