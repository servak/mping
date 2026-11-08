package panels

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// HostDetailPanel manages host detail display
type HostDetailPanel struct {
	view           *tview.TextView
	container      *tview.Flex // Container with border
	currentHost    string
	currentMetrics stats.Metrics
	config         *shared.Config
}

// NewHostDetailPanel creates a new HostDetailPanel
func NewHostDetailPanel(config *shared.Config) *HostDetailPanel {
	view := tview.NewTextView()
	view.SetDynamicColors(true).
		SetScrollable(true)

	// Create container with border and title
	container := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, false)

	return &HostDetailPanel{
		view:      view,
		container: container,
		config:    config,
	}
}

// Update refreshes host detail display for current host. It is the single
// place that styles the panel and decides its title.
func (h *HostDetailPanel) Update() {
	theme := h.config.GetTheme()
	title := " Host Details "
	if h.currentMetrics != nil {
		title = " Host Details: " + tview.Escape(h.currentHost) + " "
	}
	h.container.
		SetBorder(true).
		SetTitle(fmt.Sprintf(" [%s]%s", theme.Primary, strings.TrimPrefix(title, " "))).
		SetBackgroundColor(tcell.GetColor(theme.Background)).
		SetBorderColor(tcell.GetColor(theme.Primary))
	h.view.SetBackgroundColor(tcell.GetColor(theme.Background))

	if h.currentMetrics == nil {
		h.view.SetText("Select a host to view details")
		return
	}
	h.view.SetText(FormatHostDetail(h.currentMetrics, theme))
}

// SetMetrics sets the current metrics object directly
func (h *HostDetailPanel) SetMetrics(metrics stats.Metrics) {
	h.currentMetrics = metrics
	if metrics == nil {
		h.currentHost = ""
		return
	}
	h.currentHost = metrics.GetName()
}

// GetView returns the underlying tview component
func (h *HostDetailPanel) GetView() tview.Primitive {
	return h.container
}
