package panels

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

// HostListPanel manages host list table display
type HostListPanel struct {
	table          *tview.Table
	renderState    state.RenderState
	selectionState state.SelectionState
	mm             *stats.MetricsManager
}

type HostListParams interface {
	state.RenderState
	state.SelectionState
}

// NewHostListPanel creates a new HostListPanel
func NewHostListPanel(state HostListParams, mm *stats.MetricsManager) *HostListPanel {
	table := tview.NewTable().
		SetSelectable(true, false)

	panel := &HostListPanel{
		table:          table,
		renderState:    state,
		selectionState: state,
		mm:             mm,
	}

	panel.configureTable()

	// Set initial selection to first data row to ensure header visibility
	table.Select(1, 0)

	return panel
}

// Update refreshes host list display based on current state
func (h *HostListPanel) Update() {
	// Get filtered metrics based on current state
	metrics := h.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, h.renderState.GetSortKey(), h.renderState.IsAscending())

	// Clear existing content
	h.table.Clear()
	h.configureTable() // Reapply configuration after Clear()

	// Populate table with new data
	h.populateTable(tableData)

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
func (h *HostListPanel) getFilteredMetrics() []stats.Metrics {
	metrics := h.mm.SortBy(h.renderState.GetSortKey(), h.renderState.IsAscending())
	filterText := h.renderState.GetFilter()
	if filterText == "" {
		return metrics
	}

	filtered := []stats.Metrics{}
	filterLower := strings.ToLower(filterText)
	for _, m := range metrics {
		if strings.Contains(strings.ToLower(m.Name), filterLower) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// updateSelectedHost updates the selection state based on current table selection
func (h *HostListPanel) updateSelectedHost() {
	metrics := h.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, h.renderState.GetSortKey(), h.renderState.IsAscending())
	selectedHost := h.GetSelectedHost(tableData)
	h.selectionState.SetSelectedHost(selectedHost)
}

// GetView returns the underlying tview component
func (h *HostListPanel) GetView() *tview.Table {
	return h.table
}

// GetSelectedMetric returns the currently selected metric
func (h *HostListPanel) GetSelectedMetric(tableData *shared.TableData) (stats.Metrics, bool) {
	row, _ := h.table.GetSelection()
	if row <= 0 {
		return stats.Metrics{}, false
	}
	return tableData.GetMetricAtRow(row - 1) // Subtract 1 for header
}

// GetSelectedHost returns the name of the currently selected host
func (h *HostListPanel) GetSelectedHost(tableData *shared.TableData) string {
	if metric, ok := h.GetSelectedMetric(tableData); ok {
		return metric.Name
	}
	return ""
}

// SetSelectedFunc sets the function to call when a row is selected
func (h *HostListPanel) SetSelectedFunc(fn func(row, col int)) {
	h.table.SetSelectedFunc(fn)
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

// configureTable applies all table settings in one place to prevent configuration drift
func (h *HostListPanel) configureTable() {
	h.table.
		SetBorders(false).                   // Clean look without internal borders
		SetSeparator(' ').                   // Space separator
		SetFixed(1, 0).                      // Fix header row - CRITICAL for header visibility
		SetSelectable(true, false).          // Row selection only
		SetSelectedStyle(tcell.StyleDefault. // Selection highlighting
							Background(tcell.ColorDarkGreen).
							Foreground(tcell.ColorWhite))
}

// populateTable populates table directly from TableData
func (h *HostListPanel) populateTable(tableData *shared.TableData) {
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
		if metric.Name == selectedHost {
			h.table.Select(i+1, 0) // +1 because row 0 is header
			return
		}
	}

	// If host not found, select first row
	if h.table.GetRowCount() > 1 {
		h.table.Select(1, 0)
	}
}
