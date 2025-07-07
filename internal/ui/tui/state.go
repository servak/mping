package tui

import "github.com/servak/mping/internal/stats"

// UIState manages all UI state
type UIState struct {
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

// Sort related methods
func (s *UIState) GetSortKey() stats.Key {
	return s.sortKey
}

func (s *UIState) SetSortKey(key stats.Key) {
	s.sortKey = key
}

func (s *UIState) IsAscending() bool {
	return s.ascending
}

func (s *UIState) ReverseSort() {
	s.ascending = !s.ascending
}

// Filter related methods
func (s *UIState) GetFilter() string {
	return s.filterText
}

func (s *UIState) SetFilter(filter string) {
	s.filterText = filter
}

func (s *UIState) ClearFilter() {
	s.filterText = ""
}

// Selection related methods
func (s *UIState) GetSelectedHost() string {
	return s.selectedHost
}

func (s *UIState) SetSelectedHost(host string) {
	s.selectedHost = host
}