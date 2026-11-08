package panels

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
)

// Regression: the empty state skipped styling, and Update overwrote the
// host title that SetMetrics had set.
func TestHostDetailPanelStyleAndTitle(t *testing.T) {
	cfg := shared.DefaultConfig()
	p := NewHostDetailPanel(cfg)
	theme := cfg.GetTheme()

	p.Update() // empty state
	if p.container.GetBorderColor() != tcell.GetColor(theme.Primary) {
		t.Error("empty state did not get the themed border color")
	}
	if p.container.GetBackgroundColor() != tcell.GetColor(theme.Background) {
		t.Error("empty state did not get the themed background")
	}
	if title := p.container.GetTitle(); strings.Contains(title, ":") {
		t.Errorf("empty state title = %q", title)
	}

	mm := statstest.NewQuietManager()
	statstest.Register(t, mm, "web[1]")
	ms := mm.SortBy(stats.Success, true)
	if len(ms) != 1 {
		t.Fatal("no metrics")
	}
	p.SetMetrics(ms[0])
	p.Update()
	if title := p.container.GetTitle(); !strings.Contains(title, "Host Details: web[1[]") {
		t.Errorf("title after Update = %q, want escaped host name", title)
	}
	p.SetMetrics(nil)
	p.Update()
	if title := p.container.GetTitle(); strings.Contains(title, "web") {
		t.Errorf("title not reset: %q", title)
	}
}
