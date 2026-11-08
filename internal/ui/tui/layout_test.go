package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

func newTestLayout() *LayoutManager {
	return NewLayoutManager(state.NewUIState(), statstest.NewQuietManager(), nil, shared.DefaultConfig(), time.Second, time.Second)
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

// The path view traces like the selected probe flows: ICMP for ICMP targets,
// UDP (all ECMP branches) for every other prober kind.
func TestPathViewProtocolFollowsProbeType(t *testing.T) {
	tests := []struct {
		target    string
		probeType prober.ProbeType
		want      trace.Proto
	}{
		{"127.0.0.1", "icmpv4", trace.ProtoICMP},
		{"::1", "icmpv6", trace.ProtoICMP},
		{"tcp://127.0.0.1:443", "tcp", trace.ProtoUDP},
		{"https://127.0.0.1/", "https", trace.ProtoUDP},
		{"dns://127.0.0.1/example.com", "dns", trace.ProtoUDP},
		{"unregistered", "", trace.ProtoICMP},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			mm := statstest.NewQuietManager()
			statstest.RegisterProbe(t, mm, tt.target, tt.probeType)
			l := NewLayoutManager(state.NewUIState(), mm, nil, shared.DefaultConfig(), time.Second, time.Second)
			defer l.StopPathTrace()
			l.TogglePathView()
			l.UpdateAll()

			l.pathTracer.mu.Lock()
			target, proto := l.pathTracer.target, l.pathTracer.proto
			l.pathTracer.mu.Unlock()
			if target != tt.target || proto != tt.want {
				t.Errorf("tracing %q with %q, want %q with %q", target, proto, tt.target, tt.want)
			}
		})
	}
}

func detailText(l *LayoutManager) string {
	return l.hostDetail.GetView().(*tview.Flex).GetItem(0).(*tview.TextView).GetText(true)
}

// Regression: when the filter matched nothing the detail panel kept showing
// the previous host.
func TestDetailClearedWhenNoHostListed(t *testing.T) {
	mm := statstest.NewQuietManager()
	statstest.Register(t, mm, "gone.example")
	st := state.NewUIState()
	l := NewLayoutManager(st, mm, nil, shared.DefaultConfig(), time.Second, time.Second)
	l.ToggleDetailView()

	l.UpdateAll()
	if !strings.Contains(detailText(l), "Total Probes") {
		t.Fatalf("detail should show the host:\n%s", detailText(l))
	}

	st.SetFilter("nomatch")
	l.UpdateAll()
	if got := detailText(l); strings.Contains(got, "Total Probes") || !strings.Contains(got, "Select a host") {
		t.Errorf("detail still shows the old host:\n%s", got)
	}
}

// Regression: theme changes did not reach the filter label or help modal.
func TestThemeChangeRestylesFilterAndModal(t *testing.T) {
	cfg := shared.DefaultConfig()
	l := NewLayoutManager(state.NewUIState(), statstest.NewQuietManager(), nil, cfg, time.Second, time.Second)
	modal := tview.NewModal()
	l.RegisterThemedModal(modal)

	cfg.CycleTheme()
	l.ApplyTheme()

	theme := cfg.GetTheme()
	fg, _, _ := l.filterInput.GetLabelStyle().Decompose()
	if fg != tcell.GetColor(theme.Primary) {
		t.Errorf("filter label color = %v, want %v", fg, tcell.GetColor(theme.Primary))
	}
	if _, got, _ := l.filterInput.GetFieldStyle().Decompose(); got != tcell.GetColor(theme.SelectionBg) {
		t.Errorf("filter field background = %v, want %v", got, tcell.GetColor(theme.SelectionBg))
	}
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	modal.SetText("text").AddButtons([]string{"Close"})
	modal.SetRect(0, 0, 80, 24)
	modal.Draw(screen)
	screen.Show()
	cells, _, _ := screen.GetContents()
	found := false
	for _, c := range cells {
		if _, bg, _ := c.Style.Decompose(); bg == tcell.GetColor(theme.Background) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("modal not drawn with theme background %s", theme.Background)
	}
}
