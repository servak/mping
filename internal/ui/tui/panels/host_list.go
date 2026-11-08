package panels

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

// HostListPanel manages host list table display
type HostListPanel struct {
	table             *tview.Table
	container         *tview.Flex // Container with border
	renderState       state.RenderState
	selectionState    state.SelectionState
	mm                stats.MetricsProvider
	silence           SilenceChecker // may be nil: no host is marked
	config            *shared.Config
	onSelectionChange func(metrics stats.Metrics) // Callback when selection changes
	lastTableData     *shared.TableData           // Data rendered by the last Update
	keepSelectedHost  bool                        // Select() moves the cursor without changing the selected host
}

type HostListParams interface {
	state.RenderState
	state.SelectionState
}

// SilenceChecker reports whether a host's (display name) failure beep is muted
type SilenceChecker interface {
	IsSilenced(name string) bool
}

// silenceMarker is appended to the host name of silenced targets
const silenceMarker = " ⊘"

// NewHostListPanel creates a new HostListPanel
// silence may be nil, in which case no host is marked as silenced.
func NewHostListPanel(state HostListParams, mm stats.MetricsProvider, silence SilenceChecker, config *shared.Config) *HostListPanel {
	table := tview.NewTable().
		SetSelectable(true, false)

	// Create container with border and title
	container := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true)

	panel := &HostListPanel{
		table:          table,
		container:      container,
		renderState:    state,
		selectionState: state,
		mm:             mm,
		silence:        silence,
		config:         config,
	}
	// Every selection change (keys, mouse clicks, programmatic Select) goes
	// through here so the selection follows the host, not the row index.
	table.SetSelectionChangedFunc(func(row, _ int) {
		panel.onTableSelectionChanged(row)
	})

	return panel
}

// Update refreshes host list display based on current state
func (h *HostListPanel) Update() {
	// Get filtered metrics based on current state
	metrics := h.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, h.renderState.GetSortKey(), h.renderState.IsAscending())

	// Clear existing content and repopulate
	h.table.Clear()

	// Configure table settings with theme-aware colors
	theme := h.config.GetTheme()
	h.container.
		SetBorder(true).
		SetTitle(fmt.Sprintf(" [%s]Host List ", theme.Primary)).
		SetBackgroundColor(tcell.GetColor(theme.Background)).
		SetBorderColor(tcell.GetColor(theme.Primary))

	h.table.
		SetBorders(false).
		SetSeparator(' ').
		SetFixed(1, 0).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.GetColor(theme.SelectionBg)).
			Foreground(tcell.GetColor(theme.SelectionFg))).
		SetBackgroundColor(tcell.GetColor(theme.Background))

	// Use TableData's logic but populate our existing table
	h.populateTableFromData(tableData)
	h.lastTableData = tableData

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
	return shared.FilterMetrics(metrics, h.renderState.GetFilter())
}

// onTableSelectionChanged records the host at the selected row as the
// selection, so later updates keep selecting that host even when sorting
// moves it to another row.
func (h *HostListPanel) onTableSelectionChanged(row int) {
	if h.keepSelectedHost || h.lastTableData == nil {
		return
	}
	metric, ok := h.lastTableData.GetMetricAtRow(row - 1) // Subtract 1 for header
	if !ok {
		return
	}
	if h.selectionState.GetSelectedHost() == metric.GetName() {
		return
	}
	h.selectionState.SetSelectedHost(metric.GetName())
	if h.onSelectionChange != nil {
		h.onSelectionChange(metric)
	}
}

// CurrentSelectedMetric returns the selected host's metrics from the last
// Update. Metrics are snapshots, so this is how other panels get fresh data
// without querying the MetricsProvider again.
func (h *HostListPanel) CurrentSelectedMetric() (stats.Metrics, bool) {
	if h.lastTableData == nil {
		return nil, false
	}
	return h.GetSelectedMetric(h.lastTableData)
}

// GetView returns the underlying tview component
func (h *HostListPanel) GetView() tview.Primitive {
	return h.container
}

// GetSelectedMetric returns the currently selected metric
func (h *HostListPanel) GetSelectedMetric(tableData *shared.TableData) (stats.Metrics, bool) {
	row, _ := h.table.GetSelection()
	if row <= 0 {
		return nil, false
	}
	return tableData.GetMetricAtRow(row - 1) // Subtract 1 for header
}

// SetSelectedFunc sets the function to call when a row is selected
func (h *HostListPanel) SetSelectedFunc(fn func(row, col int)) {
	h.table.SetSelectedFunc(fn)
}

// SetSelectionChangeCallback sets the callback for when selection changes
func (h *HostListPanel) SetSelectionChangeCallback(fn func(metrics stats.Metrics)) {
	h.onSelectionChange = fn
}

// Navigation methods
func (h *HostListPanel) ScrollDown() {
	row, _ := h.table.GetSelection()
	if row+1 < h.table.GetRowCount() {
		h.table.Select(row+1, 0)
	}
}

func (h *HostListPanel) ScrollUp() {
	row, _ := h.table.GetSelection()
	if row > 1 { // Don't go above first data row (row 0 is header)
		h.table.Select(row-1, 0)
	}
}

func (h *HostListPanel) ScrollToTop() {
	h.table.Select(1, 0) // Select first data row (row 0 is header)
}

func (h *HostListPanel) ScrollToBottom() {
	rowCount := h.table.GetRowCount()
	if rowCount > 1 {
		h.table.Select(rowCount-1, 0)
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
}

// listColumn is a column of the TUI host list
type listColumn struct {
	header string
	align  int
	host   bool // the host name column, which carries the silence marker
	cell   func(r shared.Row) string
}

// tviewAlign maps a shared column alignment to tview's
func tviewAlign(a shared.Align) int {
	switch a {
	case shared.AlignRight:
		return tview.AlignRight
	case shared.AlignCenter:
		return tview.AlignCenter
	}
	return tview.AlignLeft
}

// listColumns derives the TUI columns from the shared table columns: the
// TUI-only History sparkline is inserted after Loss, and the LastFailTime
// cell also shows the duration of the related outage (with color tags).
func listColumns(td *shared.TableData, theme *shared.Theme, now time.Time) []listColumn {
	cols := make([]listColumn, 0, len(td.Columns)+1)
	for i, c := range td.Columns {
		lc := listColumn{
			header: td.Headers[i],
			align:  tviewAlign(c.Align),
			host:   c.ID == shared.ColumnHost,
			cell:   func(r shared.Row) string { return tview.Escape(r.Cells[i]) }, // host names and fail reasons are remote data
		}
		if c.ID == shared.ColumnLastFailTime {
			lc.cell = func(r shared.Row) string { return FormatLastFailCell(r.Metric, now, theme) }
		}
		cols = append(cols, lc)
		if c.ID == shared.ColumnLoss {
			cols = append(cols, listColumn{
				header: "History",
				align:  tview.AlignLeft,
				cell: func(r shared.Row) string {
					return FormatSparkline(r.Metric.GetRecentHistory(stats.DefaultHistorySize), HistoryWidth, theme)
				},
			})
		}
	}
	return cols
}

// populateTableFromData populates our table using TableData content
func (h *HostListPanel) populateTableFromData(tableData *shared.TableData) {
	// Get theme for theme-aware colors
	theme := h.config.GetTheme()
	cols := listColumns(tableData, theme, time.Now())

	setCell := func(row, col int, text, color string, header bool) {
		h.table.SetCell(row, col, &tview.TableCell{
			Text:            "  " + text + "  ",
			Color:           tcell.GetColor(color),
			BackgroundColor: tcell.GetColor(theme.Background),
			Align:           cols[col].align,
			NotSelectable:   header,
		})
	}

	// Set headers
	for col, c := range cols {
		setCell(0, col, c.header, theme.TableHeader, true)
	}

	// Set data rows
	for row, r := range tableData.Rows {
		silenced := h.silence != nil && h.silence.IsSilenced(r.Metric.GetName())
		for col, c := range cols {
			text, color := c.cell(r), theme.Primary
			if c.host && silenced {
				text += silenceMarker
				color = theme.Warning
			}
			setCell(row+1, col, text, color, false)
		}
	}
}

// restoreSelection finds and selects the row containing the specified host
func (h *HostListPanel) restoreSelection(tableData *shared.TableData, selectedHost string) {
	for i, r := range tableData.Rows {
		if r.Metric.GetName() == selectedHost {
			h.table.Select(i+1, 0) // +1 because row 0 is header
			return
		}
	}

	// If host not found (e.g. hidden by a filter), show the first row but
	// keep the selected host so it is selected again once it reappears
	if h.table.GetRowCount() > 1 {
		h.keepSelectedHost = true
		h.table.Select(1, 0)
		h.keepSelectedHost = false
	}
}
