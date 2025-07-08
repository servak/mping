package tui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/panels"
	"github.com/servak/mping/internal/ui/tui/state"
)

// DisplayMode represents the current layout mode
type DisplayMode int

const (
	ListOnly DisplayMode = iota
	ListWithDetail
)

// LayoutManager manages screen layout and panel arrangement
type LayoutManager struct {
	// Core layout components
	root  *tview.Flex
	pages *tview.Pages

	// Panels
	header     *panels.HeaderPanel
	hostList   *panels.HostListPanel
	footer     *panels.FooterPanel
	hostDetail *panels.HostDetailPanel

	// Filter input
	filterInput *tview.InputField
	showFilter  bool

	// Current layout state
	mode DisplayMode

	// Callbacks
	focusCallback func()
}

// NewLayoutManager creates a new LayoutManager
func NewLayoutManager(uiState *state.UIState, mm stats.MetricsManagerInterface, config *shared.Config, interval, timeout time.Duration) *LayoutManager {
	layout := &LayoutManager{
		mode: ListOnly,
	}

	layout.setupPanels(uiState, mm, config, interval, timeout)
	layout.setupLayout()
	layout.setupPages()

	return layout
}

// setupPanels initializes all panels
func (l *LayoutManager) setupPanels(uiState *state.UIState, mm stats.MetricsManagerInterface, config *shared.Config, interval, timeout time.Duration) {
	l.header = panels.NewHeaderPanel(uiState, config, interval, timeout)
	l.hostList = panels.NewHostListPanel(uiState, mm)
	l.footer = panels.NewFooterPanel(config)
	l.hostDetail = panels.NewHostDetailPanel(mm)

	// Setup filter input
	l.filterInput = tview.NewInputField().
		SetLabel("Filter: ").
		SetLabelColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorBlack)
}

// setupLayout configures the main layout structure
func (l *LayoutManager) setupLayout() {
	// Start with single pane layout (host list only)
	l.root = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(l.header.GetView(), 1, 0, false).
		AddItem(l.hostList.GetView(), 0, 1, true).
		AddItem(l.footer.GetView(), 1, 0, false)
}

// setupPages configures pages for modal support
func (l *LayoutManager) setupPages() {
	l.pages = tview.NewPages()
	l.pages.AddPage("main", l.root, true, true)
}

// GetRoot returns the root primitive for the application
func (l *LayoutManager) GetRoot() tview.Primitive {
	return l.pages
}

// GetHostListPanel returns the host list panel
func (l *LayoutManager) GetHostListPanel() *panels.HostListPanel {
	return l.hostList
}

// GetHostDetailPanel returns the host detail panel
func (l *LayoutManager) GetHostDetailPanel() *panels.HostDetailPanel {
	return l.hostDetail
}

// SetSelectedHost updates the detail panel with the selected host
func (l *LayoutManager) SetSelectedHost(hostname string) {
	l.hostDetail.SetHost(hostname)
}

// SetSelectedMetrics updates the detail panel with the selected metrics
func (l *LayoutManager) SetSelectedMetrics(metrics stats.MetricsReader) {
	l.hostDetail.SetMetrics(metrics)
}

// ToggleDetailView switches between single and dual pane layout
func (l *LayoutManager) ToggleDetailView() {
	if l.mode == ListOnly {
		l.showDetailView()
	} else {
		l.hideDetailView()
	}
}

// showDetailView switches to dual pane layout
func (l *LayoutManager) showDetailView() {
	l.mode = ListWithDetail
	
	// Get currently selected metrics and set them in the detail panel
	selectedMetrics := l.hostList.GetSelectedMetrics()
	if selectedMetrics != nil {
		l.hostDetail.SetMetrics(selectedMetrics)
	}
	
	// Create horizontal layout for host list and detail
	mainContent := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(l.hostList.GetView(), 0, 2, true).    // Host list takes 2/3
		AddItem(l.hostDetail.GetView(), 0, 1, false)  // Detail takes 1/3

	// Rebuild root layout with dual pane
	l.root.Clear()
	l.root.AddItem(l.header.GetView(), 1, 0, false).
		AddItem(mainContent, 0, 1, true).
		AddItem(l.footer.GetView(), 1, 0, false)
}

// hideDetailView switches to single pane layout
func (l *LayoutManager) hideDetailView() {
	l.mode = ListOnly
	
	// Rebuild root layout with single pane
	l.root.Clear()
	l.root.AddItem(l.header.GetView(), 1, 0, false).
		AddItem(l.hostList.GetView(), 0, 1, true).
		AddItem(l.footer.GetView(), 1, 0, false)
}

// UpdateAll refreshes all panels
func (l *LayoutManager) UpdateAll() {
	l.header.Update()
	l.footer.Update()
	l.hostList.Update()
	
	// Only update detail panel when it's visible
	if l.mode == ListWithDetail {
		l.hostDetail.Update()
	}
}

// HandleKeyEvent handles key events for navigation
func (l *LayoutManager) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'j':
		l.hostList.ScrollDown()
		return nil
	case 'k':
		l.hostList.ScrollUp()
		return nil
	case 'g':
		l.hostList.ScrollToTop()
		return nil
	case 'G':
		l.hostList.ScrollToBottom()
		return nil
	case 'u':
		l.hostList.PageUp()
		return nil
	case 'd':
		l.hostList.PageDown()
		return nil
	}
	return event
}

// Filter input handling methods
func (l *LayoutManager) showFilterInput() {
	if l.showFilter {
		return
	}

	l.showFilter = true

	// Rebuild layout with filter input
	l.root.Clear()
	l.root.AddItem(l.header.GetView(), 1, 0, false).
		AddItem(l.hostList.GetView(), 0, 1, false).
		AddItem(l.filterInput, 1, 0, true).
		AddItem(l.footer.GetView(), 1, 0, false)
}

func (l *LayoutManager) hideFilterInput() {
	if !l.showFilter {
		return
	}

	l.showFilter = false

	// Rebuild layout without filter input
	l.root.Clear()
	l.root.AddItem(l.header.GetView(), 1, 0, false).
		AddItem(l.hostList.GetView(), 0, 1, true).
		AddItem(l.footer.GetView(), 1, 0, false)
}

// SetFilterDoneFunc sets the function to call when filter input is done
func (l *LayoutManager) SetFilterDoneFunc(fn func(key tcell.Key)) {
	l.filterInput.SetDoneFunc(fn)
}

// GetFilterInput returns the filter input field
func (l *LayoutManager) GetFilterInput() *tview.InputField {
	return l.filterInput
}

// IsFilterShown returns whether filter input is currently shown
func (l *LayoutManager) IsFilterShown() bool {
	return l.showFilter
}

// HideFilterInput hides the filter input
func (l *LayoutManager) HideFilterInput() {
	l.hideFilterInput()
}

// SetFilterText sets the filter input text
func (l *LayoutManager) SetFilterText(text string) {
	l.filterInput.SetText(text)
}

// GetFilterText returns the current filter input text
func (l *LayoutManager) GetFilterText() string {
	return l.filterInput.GetText()
}

// SetFocusCallback sets callback function to restore focus to main view
func (l *LayoutManager) SetFocusCallback(callback func()) {
	l.focusCallback = callback
}

// RestoreFocus calls the focus callback if set
func (l *LayoutManager) RestoreFocus() {
	if l.focusCallback != nil {
		l.focusCallback()
	}
}

// Modal support methods
func (l *LayoutManager) AddModal(name string, modal tview.Primitive) {
	l.pages.AddPage(name, modal, false, false)
}

func (l *LayoutManager) RemoveModal(name string) {
	l.pages.RemovePage(name)
}

func (l *LayoutManager) AddPage(name string, page tview.Primitive, resize, visible bool) {
	l.pages.AddPage(name, page, resize, visible)
}

func (l *LayoutManager) RemovePage(name string) {
	l.pages.RemovePage(name)
}

func (l *LayoutManager) ShowPage(name string) {
	l.pages.ShowPage(name)
}

func (l *LayoutManager) HidePage(name string) {
	l.pages.HidePage(name)
}

func (l *LayoutManager) HasPage(name string) bool {
	return l.pages.HasPage(name)
}

func (l *LayoutManager) GetFrontPage() (string, tview.Primitive) {
	return l.pages.GetFrontPage()
}

// Future layout mode switching methods
func (l *LayoutManager) SetDisplayMode(mode DisplayMode) {
	l.mode = mode
	// Implementation for future layout switching
}

func (l *LayoutManager) GetDisplayMode() DisplayMode {
	return l.mode
}
