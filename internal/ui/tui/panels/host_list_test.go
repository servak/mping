package panels

import (
	"fmt"
	"testing"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// mockState implements the required interfaces for testing
type mockState struct {
	sortKey      stats.Key
	ascending    bool
	filter       string
	selectedHost string
}

func (m *mockState) GetSortKey() stats.Key       { return m.sortKey }
func (m *mockState) SetSortKey(key stats.Key)    { m.sortKey = key }
func (m *mockState) IsAscending() bool           { return m.ascending }
func (m *mockState) ReverseSort()                { m.ascending = !m.ascending }
func (m *mockState) GetFilter() string           { return m.filter }
func (m *mockState) SetFilter(filter string)     { m.filter = filter }
func (m *mockState) ClearFilter()                { m.filter = "" }
func (m *mockState) GetSelectedHost() string     { return m.selectedHost }
func (m *mockState) SetSelectedHost(host string) { m.selectedHost = host }

func newMockState() *mockState {
	return &mockState{
		sortKey:      stats.Success,
		ascending:    false,
		filter:       "",
		selectedHost: "",
	}
}

func TestNewHostListPanel(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()

	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	if panel == nil {
		t.Fatal("NewHostListPanel() returned nil")
	}

	if panel.table == nil {
		t.Error("Expected table to be initialized")
	}

	if panel.renderState == nil {
		t.Error("Expected renderState to be set")
	}

	if panel.selectionState == nil {
		t.Error("Expected selectionState to be set")
	}

	if panel.mm == nil {
		t.Error("Expected metrics manager to be set")
	}
}

func TestHostListPanelGetView(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	view := panel.GetView()
	if view == nil {
		t.Error("GetView() returned nil")
	}

	if view != panel.container {
		t.Error("GetView() returned different container instance")
	}
}

func TestHostListPanelUpdate(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register some test metrics
	mm.Register("google.com", "google.com")
	mm.Register("example.com", "example.com")

	// Note: We just register the metrics for testing
	// The actual metrics data would be populated by the probe manager

	// Test that Update() doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Update() panicked: %v", r)
		}
	}()

	panel.Update()

	// Basic validation that table has content
	if panel.table.GetRowCount() < 1 {
		t.Error("Expected table to have at least header row after Update()")
	}
}

func TestHostListPanelUpdateWithFilter(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register test metrics
	mm.Register("google.com", "google.com")
	mm.Register("yahoo.com", "yahoo.com")
	mm.Register("example.org", "example.org")

	// Set filter
	state.SetFilter("google")

	// Update should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Update() with filter panicked: %v", r)
		}
	}()

	panel.Update()
}

func TestHostListPanelUpdateSelectedHost(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register test metrics
	mm.Register("test.com", "test.com")

	// Test updateSelectedHost doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("updateSelectedHost() panicked: %v", r)
		}
	}()

	panel.updateSelectedHost()
}

func TestHostListPanelScrollDown(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register multiple metrics to enable scrolling
	for i := 0; i < 10; i++ {
		target := fmt.Sprintf("host%d.com", i)
		mm.Register(target, target)
	}

	panel.Update()

	// Test that ScrollDown doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ScrollDown() panicked: %v", r)
		}
	}()

	panel.ScrollDown()
}

func TestHostListPanelScrollUp(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register multiple metrics
	for i := 0; i < 10; i++ {
		target := fmt.Sprintf("host%d.com", i)
		mm.Register(target, target)
	}

	panel.Update()

	// Test that ScrollUp doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ScrollUp() panicked: %v", r)
		}
	}()

	panel.ScrollUp()
}

func TestHostListPanelScrollToTop(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register metrics
	mm.Register("test.com", "test.com")
	panel.Update()

	// Test that ScrollToTop doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ScrollToTop() panicked: %v", r)
		}
	}()

	panel.ScrollToTop()
}

func TestHostListPanelScrollToBottom(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register metrics
	mm.Register("test.com", "test.com")
	panel.Update()

	// Test that ScrollToBottom doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ScrollToBottom() panicked: %v", r)
		}
	}()

	panel.ScrollToBottom()
}

func TestHostListPanelPageDown(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register metrics
	mm.Register("test.com", "test.com")
	panel.Update()

	// Test that PageDown doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PageDown() panicked: %v", r)
		}
	}()

	panel.PageDown()
}

func TestHostListPanelPageUp(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register metrics
	mm.Register("test.com", "test.com")
	panel.Update()

	// Test that PageUp doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PageUp() panicked: %v", r)
		}
	}()

	panel.PageUp()
}

func TestHostListPanelRestoreSelection(t *testing.T) {
	mm := stats.NewMetricsManager()
	state := newMockState()
	config := shared.DefaultConfig()
	panel := NewHostListPanel(state, mm, config)

	// Register test metrics
	mm.Register("google.com", "google.com")
	mm.Register("example.com", "example.com")

	metrics := []stats.Metrics{
		stats.NewMetrics("google.com", 1),
		stats.NewMetrics("example.com", 1),
	}
	tableData := shared.NewTableData(metrics, stats.Success, false)

	// Test restoreSelection doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("restoreSelection() panicked: %v", r)
		}
	}()

	panel.restoreSelection(tableData, "google.com")
	panel.restoreSelection(tableData, "nonexistent.com")
}
