package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

// HeaderPanel manages header display
type HeaderPanel struct {
	view        *tview.TextView
	renderState state.RenderState
	config      *shared.Config
	interval    time.Duration
	timeout     time.Duration
}

// NewHeaderPanel creates a new HeaderPanel
func NewHeaderPanel(renderState state.RenderState, config *shared.Config, interval, timeout time.Duration) *HeaderPanel {
	view := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	return &HeaderPanel{
		view:        view,
		renderState: renderState,
		config:      config,
		interval:    interval,
		timeout:     timeout,
	}
}

// Update refreshes header display based on current state
func (h *HeaderPanel) Update() {
	content := h.generateHeaderContent()
	h.view.SetText(content)
}

// generateHeaderContent generates header text from current state
func (h *HeaderPanel) generateHeaderContent() string {
	sortDisplay := h.renderState.GetSortKey().String()
	filterText := h.renderState.GetFilter()
	
	var parts []string
	if h.config.EnableColors && h.config.Colors.Header != "" {
		parts = append(parts, fmt.Sprintf("[%s]Sort: %s[-]", h.config.Colors.Header, sortDisplay))
		parts = append(parts, fmt.Sprintf("[%s]Interval: %dms[-]", h.config.Colors.Header, h.interval.Milliseconds()))
		parts = append(parts, fmt.Sprintf("[%s]Timeout: %dms[-]", h.config.Colors.Header, h.timeout.Milliseconds()))
		
		if filterText != "" {
			parts = append(parts, fmt.Sprintf("[%s]Filter: %s[-]", h.config.Colors.Warning, filterText))
		}
		
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", h.config.Colors.Header, h.config.Title))
	} else {
		parts = append(parts, fmt.Sprintf("Sort: %s", sortDisplay))
		parts = append(parts, fmt.Sprintf("Interval: %dms", h.interval.Milliseconds()))
		parts = append(parts, fmt.Sprintf("Timeout: %dms", h.timeout.Milliseconds()))
		
		if filterText != "" {
			parts = append(parts, fmt.Sprintf("Filter: %s", filterText))
		}
		
		parts = append(parts, h.config.Title)
	}
	
	return strings.Join(parts, "    ")
}

// GetView returns the underlying tview component
func (h *HeaderPanel) GetView() *tview.TextView {
	return h.view
}

// FooterPanel manages footer display
type FooterPanel struct {
	view   *tview.TextView
	config *shared.Config
}

// NewFooterPanel creates a new FooterPanel
func NewFooterPanel(config *shared.Config) *FooterPanel {
	view := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	return &FooterPanel{
		view:   view,
		config: config,
	}
}

// Update refreshes footer display based on current state
func (f *FooterPanel) Update() {
	content := f.generateFooterContent()
	f.view.SetText(content)
}

// generateFooterContent generates footer text
func (f *FooterPanel) generateFooterContent() string {
	if f.config.EnableColors && f.config.Colors.Footer != "" {
		helpText := fmt.Sprintf("[%s]h:help[-]", f.config.Colors.Footer)
		quitText := fmt.Sprintf("[%s]q:quit[-]", f.config.Colors.Footer)
		sortText := fmt.Sprintf("[%s]s:sort[-]", f.config.Colors.Footer)
		reverseText := fmt.Sprintf("[%s]r:reverse[-]", f.config.Colors.Footer)
		resetText := fmt.Sprintf("[%s]R:reset[-]", f.config.Colors.Footer)
		filterText := fmt.Sprintf("[%s]/:filter[-]", f.config.Colors.Footer)
		moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", f.config.Colors.Footer)
		return fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s", helpText, quitText, sortText, reverseText, resetText, filterText, moveText)
	} else {
		return "h:help  q:quit  s:sort  r:reverse  R:reset  /:filter  j/k/g/G/u/d:move"
	}
}

// GetView returns the underlying tview component
func (f *FooterPanel) GetView() *tview.TextView {
	return f.view
}