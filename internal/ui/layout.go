package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

// Layout manages the main screen layout
type Layout struct {
	root           *tview.Flex
	pages          *tview.Pages
	header         *tview.TextView
	tableView      *tview.Table
	footer         *tview.TextView
	filterInput    *tview.InputField
	renderer       *Renderer
	showFilter     bool
	focusCallback  func()
	selectedHost   string // Track selected host by name instead of row number
}

// NewLayout creates a new Layout
func NewLayout(renderer *Renderer) *Layout {
	layout := &Layout{
		renderer: renderer,
	}
	
	layout.setupViews()
	layout.setupLayout()
	
	// Setup pages for modal support
	layout.pages = tview.NewPages()
	layout.pages.AddPage("main", layout.root, true, true)
	
	return layout
}

// setupViews initializes each view
func (l *Layout) setupViews() {
	// Header
	l.header = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	
	// Interactive table view
	l.tableView = tview.NewTable().
		SetSelectable(true, false).
		SetSelectedFunc(l.handleRowSelection)
	
	// Set initial selection to first data row to ensure header visibility
	l.tableView.Select(1, 0)
	
	// Footer
	l.footer = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	
	// Filter input field
	l.filterInput = tview.NewInputField().
		SetLabel("Filter: ").
		SetLabelColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetDoneFunc(l.handleFilterDone)
}

// setupLayout configures the layout
func (l *Layout) setupLayout() {
	l.root = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(l.header, 1, 0, false).
		AddItem(l.tableView, 0, 1, true).
		AddItem(l.footer, 1, 0, false)
}

// Root returns the root element of the layout
func (l *Layout) Root() tview.Primitive {
	return l.pages
}

// Update refreshes the display content
func (l *Layout) Update() {
	l.header.SetText(l.renderer.RenderHeader())
	l.footer.SetText(l.renderer.RenderFooter())
	
	// Save current selection before update
	currentRow, _ := l.tableView.GetSelection()
	if currentRow > 0 && l.selectedHost == "" { // First time or no selection yet
		tableData := l.renderer.getTableData()
		if metric, ok := tableData.GetMetricAtRow(currentRow - 1); ok {
			l.selectedHost = metric.Name
		}
	}
	
	// Update tview.Table content while preserving the original table instance
	newTable := l.renderer.GetTviewTable()
	l.tableView.Clear()
	
	// Copy all table settings from the new table to preserve styling
	l.tableView.SetBorders(false).
		SetSeparator(' ').
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDarkGreen).
			Foreground(tcell.ColorWhite))
	
	// Copy content from new table to existing table
	rows := newTable.GetRowCount()
	cols := newTable.GetColumnCount()
	
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := newTable.GetCell(row, col)
			l.tableView.SetCell(row, col, cell)
		}
	}
	
	// Restore selection based on host name
	if l.selectedHost != "" {
		l.restoreSelectionByHost()
	}
}

// HandleKeyEvent handles key events
func (l *Layout) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case '/':
		l.showFilterInput()
		return nil
	case 'j':
		l.scrollDown()
		return nil
	case 'k':
		l.scrollUp()
		return nil
	case 'g':
		l.scrollToTop()
		return nil
	case 'G':
		l.scrollToBottom()
		return nil
	case 'u':
		l.pageUp()
		return nil
	case 'd':
		l.pageDown()
		return nil
	}
	return event
}

// restoreSelectionByHost finds and selects the row containing the selected host
func (l *Layout) restoreSelectionByHost() {
	if l.selectedHost == "" {
		return
	}
	
	tableData := l.renderer.getTableData()
	for i, metric := range tableData.Metrics {
		if metric.Name == l.selectedHost {
			l.tableView.Select(i+1, 0) // +1 because row 0 is header
			return
		}
	}
	
	// If host not found, select first row
	if l.tableView.GetRowCount() > 1 {
		l.tableView.Select(1, 0)
		if len(tableData.Metrics) > 0 {
			l.selectedHost = tableData.Metrics[0].Name
		}
	}
}

// updateSelectedHost updates the selectedHost based on current selection
func (l *Layout) updateSelectedHost() {
	currentRow, _ := l.tableView.GetSelection()
	if currentRow > 0 {
		tableData := l.renderer.getTableData()
		if metric, ok := tableData.GetMetricAtRow(currentRow - 1); ok {
			l.selectedHost = metric.Name
		}
	}
}

// Scroll operation methods for tview.Table
func (l *Layout) scrollDown() {
	row, _ := l.tableView.GetSelection()
	l.tableView.Select(row+1, 0)
	l.updateSelectedHost()
}

func (l *Layout) scrollUp() {
	row, _ := l.tableView.GetSelection()
	if row > 1 { // Don't go above first data row (row 0 is header)
		l.tableView.Select(row-1, 0)
		l.updateSelectedHost()
	}
}

func (l *Layout) scrollToTop() {
	l.tableView.Select(1, 0) // Select first data row (row 0 is header)
	l.updateSelectedHost()
}

func (l *Layout) scrollToBottom() {
	rowCount := l.tableView.GetRowCount()
	if rowCount > 1 {
		l.tableView.Select(rowCount-1, 0)
		l.updateSelectedHost()
	}
}

func (l *Layout) pageDown() {
	row, _ := l.tableView.GetSelection()
	_, _, _, height := l.tableView.GetRect()
	pageSize := height / 2 // Reasonable page size
	newRow := row + pageSize
	rowCount := l.tableView.GetRowCount()
	if newRow >= rowCount {
		newRow = rowCount - 1
	}
	l.tableView.Select(newRow, 0)
	l.updateSelectedHost()
}

func (l *Layout) pageUp() {
	row, _ := l.tableView.GetSelection()
	_, _, _, height := l.tableView.GetRect()
	pageSize := height / 2 // Reasonable page size
	newRow := row - pageSize
	if newRow < 1 { // Don't go above first data row
		newRow = 1
	}
	l.tableView.Select(newRow, 0)
	l.updateSelectedHost()
}

// Filter input handling methods
func (l *Layout) showFilterInput() {
	if l.showFilter {
		return
	}
	
	l.showFilter = true
	l.filterInput.SetText(l.renderer.GetFilter())
	
	// Rebuild layout with filter input
	l.root.Clear()
	l.root.AddItem(l.header, 1, 0, false).
		AddItem(l.tableView, 0, 1, false).
		AddItem(l.filterInput, 1, 0, true).
		AddItem(l.footer, 1, 0, false)
}

func (l *Layout) hideFilterInput() {
	if !l.showFilter {
		return
	}
	
	l.showFilter = false
	
	// Rebuild layout without filter input
	l.root.Clear()
	l.root.AddItem(l.header, 1, 0, false).
		AddItem(l.tableView, 0, 1, true).
		AddItem(l.footer, 1, 0, false)
}

func (l *Layout) handleFilterDone(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		// Apply filter
		filterText := l.filterInput.GetText()
		l.renderer.SetFilter(filterText)
		l.hideFilterInput()
		l.Update()
		if l.focusCallback != nil {
			l.focusCallback()
		}
	case tcell.KeyEscape:
		// Cancel filter input
		l.hideFilterInput()
		if l.focusCallback != nil {
			l.focusCallback()
		}
	}
}

// GetFilterInput returns the filter input field for app focus management
func (l *Layout) GetFilterInput() *tview.InputField {
	return l.filterInput
}

// IsFilterShown returns whether filter input is currently shown
func (l *Layout) IsFilterShown() bool {
	return l.showFilter
}

// SetFocusCallback sets callback function to restore focus to main view
func (l *Layout) SetFocusCallback(callback func()) {
	l.focusCallback = callback
}

// handleRowSelection handles table row selection
func (l *Layout) handleRowSelection(row, col int) {
	if row == 0 {
		return // Skip header row
	}
	
	// Get table data
	tableData := l.renderer.getTableData()
	
	// Convert table row to data row (subtract 1 for header)
	dataRow := row - 1
	if metric, ok := tableData.GetMetricAtRow(dataRow); ok {
		l.showHostDetails(metric)
	}
}

// showHostDetails displays detailed information for a selected host
func (l *Layout) showHostDetails(metric stats.Metrics) {
	// For now, just show a simple modal with host details
	// This can be expanded to a more sophisticated detail view
	detailText := fmt.Sprintf(`Host Details: %s

Total Probes: %d
Successful: %d
Failed: %d
Loss Rate: %.1f%%
Last RTT: %s
Average RTT: %s
Minimum RTT: %s
Maximum RTT: %s
Last Success: %s
Last Failure: %s
Last Error: %s`,
		metric.Name,
		metric.Total,
		metric.Successful,
		metric.Failed,
		metric.Loss,
		DurationFormater(metric.LastRTT),
		DurationFormater(metric.AverageRTT),
		DurationFormater(metric.MinimumRTT),
		DurationFormater(metric.MaximumRTT),
		TimeFormater(metric.LastSuccTime),
		TimeFormater(metric.LastFailTime),
		metric.LastFailDetail,
	)

	// Create and show modal
	modal := tview.NewModal().
		SetText(detailText).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Remove modal and restore focus
			l.pages.RemovePage("details")
		})

	l.pages.AddPage("details", modal, false, true)
}