package tui

import (
	"testing"
	"time"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
)

func TestNewTUIApp(t *testing.T) {
	mm := statstest.NewQuietManager()
	cfg := shared.DefaultConfig()
	interval := time.Second
	timeout := time.Second

	app := NewTUIApp(mm, cfg, interval, timeout)

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

	app := NewTUIApp(mm, nil, interval, timeout)

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
	app := NewTUIApp(mm, cfg, time.Second, time.Second)

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
	app := NewTUIApp(mm, cfg, time.Second, time.Second)

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
	app := NewTUIApp(mm, cfg, time.Second, time.Second)

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
	app := NewTUIApp(mm, cfg, time.Second, time.Second)

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
	app := NewTUIApp(mm, cfg, time.Second, time.Second)

	modal := app.createHelpModal()

	if modal == nil {
		t.Error("createHelpModal() returned nil")
	}
}
