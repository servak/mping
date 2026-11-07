package stats

import (
	"sort"
	"sync"
)

// SilenceSet is a thread-safe set of target names whose failure beep is
// suppressed. Probing and statistics are unaffected by silencing.
type SilenceSet struct {
	mu    sync.RWMutex
	names map[string]struct{}
}

// NewSilenceSet creates a SilenceSet containing the given names
func NewSilenceSet(names ...string) *SilenceSet {
	s := &SilenceSet{names: make(map[string]struct{}, len(names))}
	for _, n := range names {
		s.names[n] = struct{}{}
	}
	return s
}

// Add silences the name
func (s *SilenceSet) Add(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names[name] = struct{}{}
}

// Remove un-silences the name
func (s *SilenceSet) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.names, name)
}

// Toggle flips the silence state of the name and returns the new state
func (s *SilenceSet) Toggle(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.names[name]; ok {
		delete(s.names, name)
		return false
	}
	s.names[name] = struct{}{}
	return true
}

// Contains reports whether the name is silenced
func (s *SilenceSet) Contains(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.names[name]
	return ok
}

// Names returns the silenced names in sorted order
func (s *SilenceSet) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.names))
	for n := range s.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// shouldBeep decides whether a failure of the target should beep. A target
// is silenced when either its event key or its display name is in the set.
func shouldBeep(s *SilenceSet, key, displayName string) bool {
	if s == nil {
		return true
	}
	return !s.Contains(key) && !s.Contains(displayName)
}
