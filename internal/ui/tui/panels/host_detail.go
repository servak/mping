package panels

import (
	"github.com/rivo/tview"
)

// HostDetailPanel manages host detail display (future feature)
type HostDetailPanel struct {
	view *tview.TextView
}

// NewHostDetailPanel creates a new HostDetailPanel
func NewHostDetailPanel() *HostDetailPanel {
	view := tview.NewTextView()
	view.SetDynamicColors(true).
		SetScrollable(true).
		SetBorder(true).
		SetTitle("Host Details")

	return &HostDetailPanel{
		view: view,
	}
}

// Update refreshes host detail display (future feature)
func (h *HostDetailPanel) Update() {
	// This will be implemented when we add the side panel feature
	// For now, it's just a placeholder
	content := `Host Details Panel
(Future feature - will show detailed metrics for selected host)`
	
	h.view.SetText(content)
}

// GetView returns the underlying tview component
func (h *HostDetailPanel) GetView() *tview.TextView {
	return h.view
}

// SetVisible controls whether the detail panel is visible
func (h *HostDetailPanel) SetVisible(visible bool) {
	// Implementation for future layout mode switching
}