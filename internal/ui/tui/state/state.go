package state

import (
	"sync"

	"github.com/servak/mping/internal/stats"
)

// UIState manages all UI state and implements all state interfaces
type UIState struct {
	mu           sync.RWMutex // Protects concurrent access
	sortKey      stats.Key
	ascending    bool
	filterText   string
	selectedHost string
}

// NewUIState creates a new UIState with defaults
func NewUIState() *UIState {
	return &UIState{
		sortKey:      stats.Success,
		ascending:    false, // Default to descending
		filterText:   "",
		selectedHost: "",
	}
}

// SelectionState implementation
func (s *UIState) GetSelectedHost() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectedHost
}

func (s *UIState) SetSelectedHost(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectedHost = host
}

// SortState implementation
func (s *UIState) GetSortKey() stats.Key {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortKey
}

func (s *UIState) SetSortKey(key stats.Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sortKey = key
}

func (s *UIState) IsAscending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ascending
}

func (s *UIState) ReverseSort() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ascending = !s.ascending
}

// FilterState implementation
func (s *UIState) GetFilter() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filterText
}

func (s *UIState) SetFilter(filter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filterText = filter
}

func (s *UIState) ClearFilter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filterText = ""
}