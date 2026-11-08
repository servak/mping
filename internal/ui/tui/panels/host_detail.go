package panels

import (
	"fmt"

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

// Update refreshes host detail display for current host
func (h *HostDetailPanel) Update() {
	if h.currentMetrics == nil {
		h.view.SetText("Select a host to view details")
		return
	}
	theme := h.config.GetTheme()
	h.container.
		SetBorder(true).
		SetTitle(fmt.Sprintf(" [%s]Host Details ", theme.Primary)).
		SetBackgroundColor(tcell.GetColor(theme.Background)).
		SetBorderColor(tcell.GetColor(theme.Primary))

	// Format and display the host details with history
	content := FormatHostDetail(h.currentMetrics, theme)
	h.view.SetBackgroundColor(tcell.GetColor(theme.Background))
	h.view.SetText(content)
}

// SetMetrics sets the current metrics object directly
func (h *HostDetailPanel) SetMetrics(metrics stats.Metrics) {
	h.currentMetrics = metrics
	if metrics == nil {
		h.currentHost = ""
		h.container.SetTitle(" Host Details ")
		return
	}
	h.currentHost = metrics.GetName()
	h.container.SetTitle(" Host Details: " + tview.Escape(h.currentHost) + " ")
}

// GetView returns the underlying tview component
func (h *HostDetailPanel) GetView() tview.Primitive {
	return h.container
}
