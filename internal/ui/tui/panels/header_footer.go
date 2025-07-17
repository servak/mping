package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
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
	theme := h.config.GetTheme()
	content := h.generateHeaderContent()
	h.view.SetBackgroundColor(tcell.GetColor(theme.Background))
	h.view.SetText(content)
}

// generateHeaderContent generates header text from current state
func (h *HeaderPanel) generateHeaderContent() string {
	sortDisplay := h.renderState.GetSortKey().String()
	filterText := h.renderState.GetFilter()

	var parts []string
	theme := h.config.GetTheme()
	if h.config.Title != "" {
		// Use title from config if available
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", theme.Primary, h.config.Title))
	}
	parts = append(parts, fmt.Sprintf("[%s]Sort: %s[-]", theme.Accent, sortDisplay))
	parts = append(parts, fmt.Sprintf("[%s]Interval: %dms[-]", theme.Accent, h.interval.Milliseconds()))
	parts = append(parts, fmt.Sprintf("[%s]Timeout: %dms[-]", theme.Accent, h.timeout.Milliseconds()))

	if filterText != "" {
		parts = append(parts, fmt.Sprintf("[%s]Filter: %s[-]", theme.Warning, filterText))
	}
	parts = append(parts, fmt.Sprintf("[%s]Theme: %s[-]", theme.Secondary, h.config.Theme))
	sep := fmt.Sprintf("[%s] | [-]", theme.Separator)
	return strings.Join(parts, sep)
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
	theme := f.config.GetTheme()
	f.view.SetBackgroundColor(tcell.GetColor(theme.Background))
	f.view.SetText(content)
}

// generateFooterContent generates footer text
func (f *FooterPanel) generateFooterContent() string {
	theme := f.config.GetTheme()
	helpText := fmt.Sprintf("[%s]h:help[-]", theme.Secondary)
	quitText := fmt.Sprintf("[%s]q:quit[-]", theme.Secondary)
	sortText := fmt.Sprintf("[%s]s:sort[-]", theme.Secondary)
	reverseText := fmt.Sprintf("[%s]r:reverse[-]", theme.Secondary)
	resetText := fmt.Sprintf("[%s]R:reset[-]", theme.Secondary)
	filterText := fmt.Sprintf("[%s]/:filter[-]", theme.Secondary)
	themeText := fmt.Sprintf("[%s]t:theme[-]", theme.Secondary)
	moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", theme.Secondary)
	return fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s  %s", helpText, quitText, sortText, reverseText, resetText, filterText, themeText, moveText)
}

// GetView returns the underlying tview component
func (f *FooterPanel) GetView() *tview.TextView {
	return f.view
}
