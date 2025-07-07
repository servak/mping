package panels

import (
	"github.com/rivo/tview"
	
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// HostDetailPanel manages host detail display
type HostDetailPanel struct {
	view           *tview.TextView
	currentHost    string
	currentMetrics stats.MetricsReader
	mm             *stats.MetricsManager
}

// NewHostDetailPanel creates a new HostDetailPanel
func NewHostDetailPanel(mm *stats.MetricsManager) *HostDetailPanel {
	view := tview.NewTextView()
	view.SetDynamicColors(true).
		SetScrollable(true).
		SetBorder(true).
		SetTitle(" Host Details ").
		SetTitleAlign(tview.AlignCenter)

	return &HostDetailPanel{
		view: view,
		mm:   mm,
	}
}

// Update refreshes host detail display for current host
func (h *HostDetailPanel) Update() {
	if h.currentMetrics == nil {
		h.view.SetText("Select a host to view details")
		return
	}

	// Format and display the host details with history
	content := shared.FormatHostDetail(h.currentMetrics)
	h.view.SetText(content)
}

// SetHost sets the current host to display details for
func (h *HostDetailPanel) SetHost(hostname string) {
	h.currentHost = hostname
	h.view.SetTitle(" Host Details: " + hostname + " ")
}

// SetMetrics sets the current metrics object directly
func (h *HostDetailPanel) SetMetrics(metrics stats.MetricsReader) {
	h.currentMetrics = metrics
	if metrics != nil {
		h.currentHost = metrics.GetName()
		h.view.SetTitle(" Host Details: " + h.currentHost + " ")
	}
}

// GetView returns the underlying tview component
func (h *HostDetailPanel) GetView() *tview.TextView {
	return h.view
}

// SetVisible controls whether the detail panel is visible
func (h *HostDetailPanel) SetVisible(visible bool) {
	if visible {
		h.view.SetBorder(true)
	} else {
		h.view.SetBorder(false)
	}
}