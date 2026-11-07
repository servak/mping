package tui

import (
	"testing"
	"time"

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
