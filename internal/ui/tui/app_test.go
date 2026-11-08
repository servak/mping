package tui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/beep"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
)

func TestNewTUIApp(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	interval := time.Second
	timeout := time.Second

	app := NewTUIApp(mm, nil, cfg, interval, timeout)

	if app == nil {
		t.Fatal("NewTUIApp() returned nil")
	}

	if app.app == nil {
		t.Error("Expected tview application to be initialized")
	}

	if app.layout == nil {
		t.Error("Expected layout to be initialized")
	}

	if app.state == nil {
		t.Error("Expected state to be initialized")
	}

	if app.mm != mm {
		t.Error("Expected metrics manager to be set correctly")
	}

	if app.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	if app.interval != interval {
		t.Error("Expected interval to be set correctly")
	}

	if app.timeout != timeout {
		t.Error("Expected timeout to be set correctly")
	}
}

func TestNewTUIAppWithNilConfig(t *testing.T) {
	mm := statstest.NewQuietManager()
	interval := time.Second
	timeout := time.Second

	app := NewTUIApp(mm, nil, nil, interval, timeout)

	if app == nil {
		t.Fatal("NewTUIApp() with nil config returned nil")
	}

	if app.config == nil {
		t.Error("Expected default config to be used when nil is passed")
	}

	// Verify it's using default config values
	if app.config.Title != "mping" {
		t.Error("Expected default config to be applied")
	}
}

func TestTUIAppClose(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	app := NewTUIApp(mm, nil, cfg, time.Second, time.Second)

	// Test that Close doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Close() panicked: %v", r)
		}
	}()

	app.Close()
}

func TestTUIAppSortMethods(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	app := NewTUIApp(mm, nil, cfg, time.Second, time.Second)

	initialKey := app.state.GetSortKey()
	initialAscending := app.state.IsAscending()

	// Test nextSort
	app.nextSort()
	if app.state.GetSortKey() == initialKey {
		// Should change unless we're at the last key and wrap around
		keys := stats.Keys()
		if initialKey != stats.Key(len(keys)-1) {
			t.Error("nextSort() should change sort key")
		}
	}

	// Test prevSort
	app.prevSort()
	// Should return to initial or be different

	// Test reverseSort
	app.reverseSort()
	if app.state.IsAscending() == initialAscending {
		t.Error("reverseSort() should change sort order")
	}
}

func TestTUIAppResetMetrics(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	app := NewTUIApp(mm, nil, cfg, time.Second, time.Second)

	// Register test data
	statstest.Register(t, mm, "test.com")

	// Test resetMetrics doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resetMetrics() panicked: %v", r)
		}
	}()

	app.resetMetrics()
}

func TestTUIAppFilterMethods(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	app := NewTUIApp(mm, nil, cfg, time.Second, time.Second)

	// Test clearFilter
	app.state.SetFilter("test")
	app.clearFilter()
	if app.state.GetFilter() != "" {
		t.Error("clearFilter() should clear the filter")
	}
}

func TestTUIAppCreateHelpModal(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	app := NewTUIApp(mm, nil, cfg, time.Second, time.Second)

	modal := app.createHelpModal()

	if modal == nil {
		t.Error("createHelpModal() returned nil")
	}
}

func TestTUIAppBeepKeysUseNotifier(t *testing.T) {
	mm := statstest.NewQuietManager()
	statstest.Register(t, mm, "quiet")
	n := beep.NewNotifier()
	app := NewTUIApp(mm, n, shared.DefaultConfig(), time.Second, time.Second)

	app.toggleBeep()
	if n.Enabled() {
		t.Error("toggleBeep did not disable the beep")
	}

	app.layout.UpdateAll() // highlights the only row
	app.toggleSilence()
	if !n.IsSilenced("quiet") {
		t.Error("toggleSilence did not silence the selected host")
	}
}

func TestTUIAppBeepKeysWithoutNotifier(t *testing.T) {
	app := NewTUIApp(statstest.NewQuietManager(), nil, shared.DefaultConfig(), time.Second, time.Second)
	app.state.SetSelectedHost("x")
	// Must not panic
	app.toggleBeep()
	app.toggleSilence()
}

// Regression: with the selected host hidden by the filter the list highlights
// the first visible row; m used to mute the hidden selected host instead.
func TestToggleSilenceMutesHighlightedRow(t *testing.T) {
	mm := statstest.NewQuietManager()
	statstest.Register(t, mm, "visible", "hidden")
	n := beep.NewNotifier()
	app := NewTUIApp(mm, n, shared.DefaultConfig(), time.Second, time.Second)

	app.state.SetSelectedHost("hidden")
	app.state.SetFilter("visible")
	app.layout.UpdateAll()

	app.toggleSilence()
	if n.IsSilenced("hidden") {
		t.Error("toggleSilence muted the host hidden by the filter")
	}
	if !n.IsSilenced("visible") {
		t.Error("toggleSilence did not mute the highlighted host")
	}

	// Nothing listed: no-op
	app.state.SetFilter("no-such-host")
	app.layout.UpdateAll()
	app.toggleSilence()
	if !n.IsSilenced("visible") {
		t.Error("toggleSilence with an empty list must not change anything")
	}
}

// Regression: clicking the list while the filter bar was open moved focus
// away but left the bar "shown", so every key was passed through and Esc/q/v
// stopped working.
func TestKeysWorkAfterFilterLosesFocus(t *testing.T) {
	mm := statstest.NewQuietManager()
	statstest.Register(t, mm, "a")
	app := NewTUIApp(mm, nil, shared.DefaultConfig(), time.Second, time.Second)
	capture := app.app.GetInputCapture()

	app.state.SetFilter("a")
	app.showFilter()
	// While the input has focus, keys go to it
	if ev := capture(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)); ev == nil {
		t.Fatal("keys must reach the focused filter input")
	}

	// A mouse click on the list focuses the table
	app.app.SetFocus(app.layout.GetHostListPanel().GetView())

	if ev := capture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); ev != nil {
		t.Errorf("Esc was not handled: %v", ev)
	}
	if app.layout.IsFilterShown() {
		t.Error("filter bar should close once it lost focus")
	}
	if app.state.GetFilter() != "" {
		t.Errorf("Esc should clear the active filter, got %q", app.state.GetFilter())
	}
	if ev := capture(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)); ev != nil {
		t.Error("main key bindings must work again")
	}
}

// Regression: the help modal's Close button did nothing.
func TestHelpModalCloseButton(t *testing.T) {
	app := NewTUIApp(statstest.NewQuietManager(), nil, shared.DefaultConfig(), time.Second, time.Second)
	app.showHelp()
	if !app.isHelpVisible() {
		t.Fatal("help should be visible")
	}
	app.layout.GetRoot().InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) {
		app.app.SetFocus(p)
	})
	if app.isHelpVisible() {
		t.Error("Enter on the Close button should hide the help")
	}
}
