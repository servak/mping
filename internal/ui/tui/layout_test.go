package tui

import (
	"testing"
	"time"

	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

func newTestLayout() *LayoutManager {
	return NewLayoutManager(state.NewUIState(), statstest.NewQuietManager(), shared.DefaultConfig(), time.Second, time.Second)
}

// hasSidePanel reports whether the main content item is a list+side-panel
// wrapper rather than the bare host list view.
func hasSidePanel(l *LayoutManager) bool {
	return l.root.GetItem(1) != l.hostList.GetView()
}

// Regression: closing the filter input in detail/path view used to rebuild the
// list-only layout, dropping the side panel while l.mode still said it was shown.
func TestFilterToggleKeepsSidePanel(t *testing.T) {
	modes := []struct {
		name   string
		toggle func(*LayoutManager)
	}{
		{"detail", (*LayoutManager).ToggleDetailView},
		{"path", (*LayoutManager).TogglePathView},
	}
	for _, tt := range modes {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLayout()
			defer l.StopPathTrace()

			if hasSidePanel(l) || l.root.GetItemCount() != 3 {
				t.Fatalf("list-only layout: sidePanel=%v items=%d", hasSidePanel(l), l.root.GetItemCount())
			}

			tt.toggle(l)
			if !hasSidePanel(l) || l.root.GetItemCount() != 3 {
				t.Fatalf("after mode switch: sidePanel=%v items=%d", hasSidePanel(l), l.root.GetItemCount())
			}

			l.showFilterInput()
			if !hasSidePanel(l) || l.root.GetItemCount() != 4 {
				t.Errorf("filter shown: sidePanel=%v items=%d, want side panel and 4 items", hasSidePanel(l), l.root.GetItemCount())
			}

			l.hideFilterInput()
			if !hasSidePanel(l) || l.root.GetItemCount() != 3 {
				t.Errorf("filter hidden: sidePanel=%v items=%d, want side panel and 3 items", hasSidePanel(l), l.root.GetItemCount())
			}

			// Leaving the mode still returns to the plain list
			tt.toggle(l)
			if hasSidePanel(l) {
				t.Error("toggling the mode off should drop the side panel")
			}
		})
	}
}
