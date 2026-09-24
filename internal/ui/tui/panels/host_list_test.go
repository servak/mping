package panels

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
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

// feedEvents sends events through the public API and waits until processed
func feedEvents(t *testing.T, mm stats.MetricsManager, events ...*prober.Event) {
	t.Helper()
	ch := make(chan *prober.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	<-mm.Subscribe(ch)
}

func selectedName(t *testing.T, p *HostListPanel) string {
	t.Helper()
	m, ok := p.CurrentSelectedMetric()
	if !ok {
		t.Fatal("no host selected")
	}
	return m.GetName()
}

// Regression: with no explicit selection the panel used to always select
// row 1, so a sort change (e.g. another host failing once) silently moved
// the selection - and restarted the path trace - to a different host.
func TestHostListPanelSelectionFollowsHostAcrossSortChanges(t *testing.T) {
	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	st := state.NewUIState() // default sort: Fail, descending
	panel := NewHostListPanel(st, mm, shared.DefaultConfig())

	now := time.Now()
	var events []*prober.Event
	for _, h := range []string{"8.8.8.8", "1.1.1.1", "192.168.1.1"} {
		events = append(events,
			&prober.Event{Key: h, DisplayName: h, Result: prober.REGISTER},
			&prober.Event{Key: h, Result: prober.SUCCESS, SentTime: now, Rtt: time.Millisecond})
	}
	feedEvents(t, mm, events...)

	panel.Update()
	if got := selectedName(t, panel); got != "1.1.1.1" {
		t.Fatalf("initial selection = %s, want 1.1.1.1 (first by name)", got)
	}
	if got := st.GetSelectedHost(); got != "1.1.1.1" {
		t.Fatalf("default selection must be recorded in state, got %q", got)
	}

	// 8.8.8.8 times out once and jumps to the top of the Fail-sorted list
	feedEvents(t, mm, &prober.Event{Key: "8.8.8.8", Result: prober.TIMEOUT, SentTime: now, Message: "timeout"})
	panel.Update()
	if got := selectedName(t, panel); got != "1.1.1.1" {
		t.Errorf("selection moved to %s after a sort change, want it to stay on 1.1.1.1", got)
	}
	if row, _ := panel.table.GetSelection(); row == 1 {
		t.Errorf("1.1.1.1 should no longer be on row 1 (8.8.8.8 sorts first)")
	}
}

// Mouse clicks select rows through Table.Select; the selection must stick
// on the next Update instead of snapping back.
func TestHostListPanelClickSelectionSticks(t *testing.T) {
	mm := stats.NewMetricsManager()
	st := state.NewUIState()
	panel := NewHostListPanel(st, mm, shared.DefaultConfig())
	mm.Register("a.example", "a.example")
	mm.Register("b.example", "b.example")

	var notified string
	panel.SetSelectionChangeCallback(func(m stats.Metrics) { notified = m.GetName() })

	panel.Update()
	panel.table.Select(2, 0) // what tview does on a mouse click
	panel.Update()

	if got := selectedName(t, panel); got != "b.example" {
		t.Errorf("selection after click = %s, want b.example", got)
	}
	if notified != "b.example" {
		t.Errorf("selection change callback got %q, want b.example", notified)
	}
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

func TestHostListPanelCurrentSelectedMetricIsFresh(t *testing.T) {
	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	events := make(chan *prober.Event, 10)
	done := mm.Subscribe(events)
	events <- &prober.Event{Key: "h", DisplayName: "host", Result: prober.REGISTER}

	panel := NewHostListPanel(state.NewUIState(), mm, shared.DefaultConfig())
	waitFor := func(total int) {
		t.Helper()
		for i := 0; i < 100; i++ {
			panel.Update()
			if m, ok := panel.CurrentSelectedMetric(); ok && m.GetTotal() == total {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("selected metric never reached total=%d", total)
	}

	waitFor(0)
	events <- &prober.Event{Key: "h", Result: prober.SENT}
	waitFor(1) // a later Update must expose the new data, not the old snapshot
	close(events)
	<-done
}

func TestHostListPanelLastFailTimeShowsOutage(t *testing.T) {
	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	panel := NewHostListPanel(state.NewUIState(), mm, shared.DefaultConfig())

	now := time.Now()
	feedEvents(t, mm,
		&prober.Event{Key: "down", DisplayName: "down.example", Result: prober.REGISTER},
		&prober.Event{Key: "up", DisplayName: "up.example", Result: prober.REGISTER},
		&prober.Event{Key: "down", Result: prober.SUCCESS, SentTime: now.Add(-3 * time.Second)},
		&prober.Event{Key: "down", Result: prober.TIMEOUT, SentTime: now.Add(-2 * time.Second), Message: "timeout"},
		&prober.Event{Key: "down", Result: prober.TIMEOUT, SentTime: now.Add(-1 * time.Second), Message: "timeout"},
		&prober.Event{Key: "down", Result: prober.TIMEOUT, SentTime: now, Message: "timeout"},
		&prober.Event{Key: "up", Result: prober.SUCCESS, SentTime: now},
	)
	panel.Update()

	col := shared.ColumnLastFailTime + 1 // shifted by the History column
	if got := panel.table.GetCell(0, col).Text; !strings.Contains(got, "LastFailTime") {
		t.Fatalf("header at column %d = %q, want LastFailTime", col, got)
	}
	for row := 0; row < panel.table.GetRowCount(); row++ {
		for c := 0; c < panel.table.GetColumnCount(); c++ {
			if strings.Contains(panel.table.GetCell(row, c).Text, "Outage") {
				t.Errorf("there must be no separate Outage column (row %d col %d)", row, c)
			}
		}
	}
	cells := map[string]string{}
	for row := 1; row < panel.table.GetRowCount(); row++ {
		name := strings.TrimSpace(panel.table.GetCell(row, 0).Text)
		cells[name] = panel.table.GetCell(row, col).Text
	}
	if !strings.Contains(cells["down.example"], "(DOWN 2.") {
		t.Errorf("down host LastFailTime = %q, want time with (DOWN ~2s)", cells["down.example"])
	}
	if strings.TrimSpace(cells["up.example"]) != "-" {
		t.Errorf("healthy host LastFailTime = %q, want -", cells["up.example"])
	}
}

// A filter that hides the selected host shows the first row meanwhile, but
// the selection must return to the host once the filter is cleared.
func TestHostListPanelSelectionSurvivesFilter(t *testing.T) {
	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	st := state.NewUIState()
	panel := NewHostListPanel(st, mm, shared.DefaultConfig())

	now := time.Now()
	var events []*prober.Event
	for _, h := range []string{"8.8.8.8", "1.1.1.1", "192.168.1.1"} {
		events = append(events,
			&prober.Event{Key: h, DisplayName: h, Result: prober.REGISTER},
			&prober.Event{Key: h, Result: prober.SUCCESS, SentTime: now, Rtt: time.Millisecond})
	}
	feedEvents(t, mm, events...)

	st.SetSelectedHost("192.168.1.1")
	panel.Update()

	st.SetFilter("8.8")
	panel.Update()
	if got := selectedName(t, panel); got != "8.8.8.8" {
		t.Fatalf("filtered selection = %s, want 8.8.8.8 (only row)", got)
	}

	st.SetFilter("")
	panel.Update()
	if got := selectedName(t, panel); got != "192.168.1.1" {
		t.Errorf("selection after clearing the filter = %s, want 192.168.1.1", got)
	}
}
