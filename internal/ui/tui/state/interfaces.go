package state

import "github.com/servak/mping/internal/stats"

// SelectionState manages selected host state
type SelectionState interface {
	GetSelectedHost() string
	SetSelectedHost(host string)
}

// SortState manages sort-related state
type SortState interface {
	GetSortKey() stats.Key
	SetSortKey(key stats.Key)
	IsAscending() bool
	ReverseSort()
}

// FilterState manages filter-related state
type FilterState interface {
	GetFilter() string
	SetFilter(filter string)
	ClearFilter()
}

// RenderState provides read-only access for rendering
type RenderState interface {
	GetSortKey() stats.Key
	IsAscending() bool
	GetFilter() string
	GetSelectedHost() string
}

// FullUIState provides complete access to all UI state
// Only TUIApp should use this interface
type FullUIState interface {
	SelectionState
	SortState
	FilterState
	RenderState
}