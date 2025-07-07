package state

import (
	"fmt"
	"sync"
	"testing"

	"github.com/servak/mping/internal/stats"
)

func TestNewUIState(t *testing.T) {
	state := NewUIState()

	if state == nil {
		t.Fatal("NewUIState() returned nil")
	}

	// Test default values
	if state.GetSortKey() != stats.Success {
		t.Errorf("Expected default sort key to be %v, got %v", stats.Success, state.GetSortKey())
	}

	if state.IsAscending() {
		t.Error("Expected default sort order to be descending")
	}

	if state.GetFilter() != "" {
		t.Errorf("Expected default filter to be empty, got '%s'", state.GetFilter())
	}

	if state.GetSelectedHost() != "" {
		t.Errorf("Expected default selected host to be empty, got '%s'", state.GetSelectedHost())
	}
}

func TestUIStateSortKey(t *testing.T) {
	state := NewUIState()

	testCases := []stats.Key{
		stats.Host,
		stats.Sent,
		stats.Success,
		stats.Fail,
		stats.Loss,
		stats.Last,
		stats.Avg,
		stats.Best,
		stats.Worst,
	}

	for _, key := range testCases {
		state.SetSortKey(key)
		if state.GetSortKey() != key {
			t.Errorf("SetSortKey(%v) failed, got %v", key, state.GetSortKey())
		}
	}
}

func TestUIStateSortOrder(t *testing.T) {
	state := NewUIState()

	// Test initial state (should be descending)
	if state.IsAscending() {
		t.Error("Expected initial sort order to be descending")
	}

	// Test reverse
	state.ReverseSort()
	if !state.IsAscending() {
		t.Error("Expected sort order to be ascending after ReverseSort()")
	}

	// Test reverse again
	state.ReverseSort()
	if state.IsAscending() {
		t.Error("Expected sort order to be descending after second ReverseSort()")
	}
}

func TestUIStateFilter(t *testing.T) {
	state := NewUIState()

	testFilters := []string{
		"google",
		"example.com",
		"test123",
		"",
		"case-SENSITIVE-Test",
	}

	for _, filter := range testFilters {
		state.SetFilter(filter)
		if state.GetFilter() != filter {
			t.Errorf("SetFilter('%s') failed, got '%s'", filter, state.GetFilter())
		}
	}

	// Test clear filter
	state.SetFilter("some-filter")
	state.ClearFilter()
	if state.GetFilter() != "" {
		t.Errorf("ClearFilter() failed, got '%s'", state.GetFilter())
	}
}

func TestUIStateSelectedHost(t *testing.T) {
	state := NewUIState()

	testHosts := []string{
		"google.com",
		"example.org",
		"localhost",
		"192.168.1.1",
		"",
	}

	for _, host := range testHosts {
		state.SetSelectedHost(host)
		if state.GetSelectedHost() != host {
			t.Errorf("SetSelectedHost('%s') failed, got '%s'", host, state.GetSelectedHost())
		}
	}
}

func TestUIStateConcurrency(t *testing.T) {
	state := NewUIState()
	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 4) // 4 types of operations

	// Test concurrent access to sort key
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := stats.Key(j % 9) // There are 9 different sort keys
				state.SetSortKey(key)
				_ = state.GetSortKey()
			}
		}(i)
	}

	// Test concurrent access to sort order
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				state.ReverseSort()
				_ = state.IsAscending()
			}
		}()
	}

	// Test concurrent access to filter
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				filter := fmt.Sprintf("filter-%d-%d", id, j)
				state.SetFilter(filter)
				_ = state.GetFilter()
				if j%10 == 0 {
					state.ClearFilter()
				}
			}
		}(i)
	}

	// Test concurrent access to selected host
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				host := fmt.Sprintf("host-%d-%d.com", id, j)
				state.SetSelectedHost(host)
				_ = state.GetSelectedHost()
			}
		}(i)
	}

	wg.Wait()

	// If we reach here without panics or data races, the test passes
}

func TestUIStateInterfaces(t *testing.T) {
	state := NewUIState()

	// Test that UIState implements all expected interfaces
	var _ SelectionState = state
	var _ SortState = state
	var _ FilterState = state
	var _ RenderState = state
	var _ FullUIState = state
}

