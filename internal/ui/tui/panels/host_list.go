package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

// HostListPanel manages host list table display
type HostListPanel struct {
	table             *tview.Table
	container         *tview.Flex  // Container with border
	renderState       state.RenderState
	selectionState    state.SelectionState
	mm                stats.MetricsProvider
	onSelectionChange func(metrics stats.MetricsReader) // Callback when selection changes
}

type HostListParams interface {
	state.RenderState
	state.SelectionState
}

// NewHostListPanel creates a new HostListPanel
func NewHostListPanel(state HostListParams, mm stats.MetricsProvider) *HostListPanel {
	table := tview.NewTable().
		SetSelectable(true, false)

	// Create container with border and title
	container := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true)
	
	container.SetBorder(true).
		SetTitle(" Host List ")

	panel := &HostListPanel{
		table:          table,
		container:      container,
		renderState:    state,
		selectionState: state,
		mm:             mm,
	}

	return panel
}

// Update refreshes host list display based on current state
func (h *HostListPanel) Update() {
	// Get filtered metrics based on current state
	metrics := h.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, h.renderState.GetSortKey(), h.renderState.IsAscending())

	// Clear existing content and repopulate
	h.table.Clear()

	// Configure table settings
	h.table.
		SetBorders(false).
		SetSeparator(' ').
		SetFixed(1, 0).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDarkGreen).
			Foreground(tcell.ColorWhite))

	// Use TableData's logic but populate our existing table
	h.populateTableFromData(tableData)

	// Restore selection if specified
	selectedHost := h.renderState.GetSelectedHost()
	if selectedHost != "" {
		h.restoreSelection(tableData, selectedHost)
	} else {
		// Default to first data row
		if h.table.GetRowCount() > 1 {
			h.table.Select(1, 0)
		}
	}
}

// getFilteredMetrics returns filtered metrics based on current state
func (h *HostListPanel) getFilteredMetrics() []stats.MetricsReader {
	metrics := h.mm.SortByWithReader(h.renderState.GetSortKey(), h.renderState.IsAscending())
	return shared.FilterMetrics(metrics, h.renderState.GetFilter())
}

// updateSelectedHost updates the selection state based on current table selection
func (h *HostListPanel) updateSelectedHost() {
	metrics := h.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, h.renderState.GetSortKey(), h.renderState.IsAscending())
	selectedHost := h.GetSelectedHost(tableData)

	// Only update if the selection actually changed to avoid loops
	if h.selectionState.GetSelectedHost() != selectedHost {
		h.selectionState.SetSelectedHost(selectedHost)

		// Call the callback to update detail panel with metrics object
		if metric, ok := h.GetSelectedMetric(tableData); ok && h.onSelectionChange != nil {
			h.onSelectionChange(metric)
		}
	}
}

// GetView returns the underlying tview component
func (h *HostListPanel) GetView() tview.Primitive {
	return h.container
}

// GetSelectedMetric returns the currently selected metric
func (h *HostListPanel) GetSelectedMetric(tableData *shared.TableData) (stats.MetricsReader, bool) {
	row, _ := h.table.GetSelection()
	if row <= 0 {
		return nil, false
	}
	return tableData.GetMetricAtRow(row - 1) // Subtract 1 for header
}

// GetSelectedHost returns the name of the currently selected host
func (h *HostListPanel) GetSelectedHost(tableData *shared.TableData) string {
	if metric, ok := h.GetSelectedMetric(tableData); ok {
		return metric.GetName()
	}
	return ""
}

// SetSelectedFunc sets the function to call when a row is selected
func (h *HostListPanel) SetSelectedFunc(fn func(row, col int)) {
	h.table.SetSelectedFunc(fn)
}

// SetSelectionChangeCallback sets the callback for when selection changes
func (h *HostListPanel) SetSelectionChangeCallback(fn func(metrics stats.MetricsReader)) {
	h.onSelectionChange = fn
}

// Navigation methods
func (h *HostListPanel) ScrollDown() {
	row, _ := h.table.GetSelection()
	h.table.Select(row+1, 0)
	// Update selection state directly
	h.updateSelectedHost()
}

func (h *HostListPanel) ScrollUp() {
	row, _ := h.table.GetSelection()
	if row > 1 { // Don't go above first data row (row 0 is header)
		h.table.Select(row-1, 0)
		// Update selection state directly
		h.updateSelectedHost()
	}
}

func (h *HostListPanel) ScrollToTop() {
	h.table.Select(1, 0) // Select first data row (row 0 is header)
	// Update selection state directly
	h.updateSelectedHost()
}

func (h *HostListPanel) ScrollToBottom() {
	rowCount := h.table.GetRowCount()
	if rowCount > 1 {
		h.table.Select(rowCount-1, 0)
		// Update selection state directly
		h.updateSelectedHost()
	}
}

func (h *HostListPanel) PageDown() {
	row, _ := h.table.GetSelection()
	_, _, _, height := h.table.GetRect()
	pageSize := height / 2 // Reasonable page size
	newRow := row + pageSize
	rowCount := h.table.GetRowCount()
	if newRow >= rowCount {
		newRow = rowCount - 1
	}
	h.table.Select(newRow, 0)
	// Update selection state directly
	h.updateSelectedHost()
}

func (h *HostListPanel) PageUp() {
	row, _ := h.table.GetSelection()
	_, _, _, height := h.table.GetRect()
	pageSize := height / 2 // Reasonable page size
	newRow := row - pageSize
	if newRow < 1 { // Don't go above first data row
		newRow = 1
	}
	h.table.Select(newRow, 0)
	// Update selection state directly
	h.updateSelectedHost()
}

// populateTableFromData populates our table using TableData content
func (h *HostListPanel) populateTableFromData(tableData *shared.TableData) {
	// Define alignment for each column (same as in shared/table_data.go)
	alignments := []int{
		tview.AlignLeft,   // Host
		tview.AlignRight,  // Sent
		tview.AlignRight,  // Succ
		tview.AlignRight,  // Fail
		tview.AlignRight,  // Loss
		tview.AlignRight,  // Last
		tview.AlignRight,  // Avg
		tview.AlignRight,  // Best
		tview.AlignRight,  // Worst
		tview.AlignCenter, // LastSuccTime
		tview.AlignCenter, // LastFailTime
		tview.AlignLeft,   // FAIL Reason
	}

	// Set headers
	for col, header := range tableData.Headers {
		alignment := tview.AlignLeft
		if col < len(alignments) {
			alignment = alignments[col]
		}

		h.table.SetCell(0, col, &tview.TableCell{
			Text:          "  " + header + "  ",
			Color:         tcell.ColorYellow,
			Align:         alignment,
			NotSelectable: true,
		})
	}

	// Set data rows
	for row, rowData := range tableData.Rows {
		for col, cellData := range rowData {
			alignment := tview.AlignLeft
			if col < len(alignments) {
				alignment = alignments[col]
			}

			h.table.SetCell(row+1, col, &tview.TableCell{
				Text:  "  " + cellData + "  ",
				Color: tcell.ColorWhite,
				Align: alignment,
			})
		}
	}
}

// restoreSelection finds and selects the row containing the specified host
func (h *HostListPanel) restoreSelection(tableData *shared.TableData, selectedHost string) {
	for i, metric := range tableData.Metrics {
		if metric.GetName() == selectedHost {
			h.table.Select(i+1, 0) // +1 because row 0 is header
			return
		}
	}

	// If host not found, select first row
	if h.table.GetRowCount() > 1 {
		h.table.Select(1, 0)
	}
}

// GetSelectedMetrics returns the currently selected metrics
func (h *HostListPanel) GetSelectedMetrics() stats.MetricsReader {
	metrics := h.getFilteredMetrics()
	if len(metrics) == 0 {
		return nil
	}

	row, _ := h.table.GetSelection()
	// row 0 is header, so data starts from row 1
	if row >= 1 && row-1 < len(metrics) {
		return metrics[row-1]
	}

	return nil
}
