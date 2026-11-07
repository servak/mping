package shared

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/servak/mping/internal/stats"
)

// ColumnID identifies a column of the host table independently of its
// position, so renderers can locate or replace columns by name.
type ColumnID int

const (
	ColumnHost ColumnID = iota
	ColumnSent
	ColumnSucc
	ColumnFail
	ColumnLoss
	ColumnLast
	ColumnAvg
	ColumnBest
	ColumnWorst
	ColumnLastSuccTime
	ColumnLastFailTime
	ColumnFailReason
)

// Align is the preferred horizontal alignment of a column in interactive
// views. The plain-text table export does not apply it.
type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Column describes one column of the host table
type Column struct {
	ID    ColumnID
	Title string // header text without the sort arrow
	Align Align
	// SortKey is the key the column is sorted by; meaningful only if Sortable
	SortKey  stats.Key
	Sortable bool
	// Value renders the plain-text cell for a target
	Value func(m stats.Metrics) string
}

// Columns lists the host table columns in display order
var Columns = []Column{
	{ID: ColumnHost, Title: "Host", Align: AlignLeft, SortKey: stats.Host, Sortable: true,
		Value: func(m stats.Metrics) string { return m.GetName() }},
	{ID: ColumnSent, Title: "Sent", Align: AlignRight, SortKey: stats.Sent, Sortable: true,
		Value: func(m stats.Metrics) string { return fmt.Sprintf("%d", m.GetTotal()) }},
	{ID: ColumnSucc, Title: "Succ", Align: AlignRight, SortKey: stats.Success, Sortable: true,
		Value: func(m stats.Metrics) string { return fmt.Sprintf("%d", m.GetSuccessful()) }},
	{ID: ColumnFail, Title: "Fail", Align: AlignRight, SortKey: stats.Fail, Sortable: true,
		Value: func(m stats.Metrics) string { return fmt.Sprintf("%d", m.GetFailed()) }},
	{ID: ColumnLoss, Title: "Loss", Align: AlignRight, SortKey: stats.Loss, Sortable: true,
		Value: func(m stats.Metrics) string { return fmt.Sprintf("%5.1f%%", m.GetLoss()) }},
	{ID: ColumnLast, Title: "Last", Align: AlignRight, SortKey: stats.Last, Sortable: true,
		Value: func(m stats.Metrics) string { return DurationFormater(m.GetLastRTT()) }},
	{ID: ColumnAvg, Title: "Avg", Align: AlignRight, SortKey: stats.Avg, Sortable: true,
		Value: func(m stats.Metrics) string { return DurationFormater(m.GetAverageRTT()) }},
	{ID: ColumnBest, Title: "Best", Align: AlignRight, SortKey: stats.Best, Sortable: true,
		Value: func(m stats.Metrics) string { return DurationFormater(m.GetMinimumRTT()) }},
	{ID: ColumnWorst, Title: "Worst", Align: AlignRight, SortKey: stats.Worst, Sortable: true,
		Value: func(m stats.Metrics) string { return DurationFormater(m.GetMaximumRTT()) }},
	{ID: ColumnLastSuccTime, Title: "LastSuccTime", Align: AlignCenter, SortKey: stats.LastSuccTime, Sortable: true,
		Value: func(m stats.Metrics) string { return TimeFormater(m.GetLastSuccTime()) }},
	{ID: ColumnLastFailTime, Title: "LastFailTime", Align: AlignCenter, SortKey: stats.LastFailTime, Sortable: true,
		Value: func(m stats.Metrics) string { return TimeFormater(m.GetLastFailTime()) }},
	{ID: ColumnFailReason, Title: "FAIL Reason", Align: AlignLeft,
		Value: func(m stats.Metrics) string { return m.GetLastFailDetail() }},
}

// Header returns the column title with a sort direction arrow when the table
// is sorted by this column
func (c Column) Header(sortKey stats.Key, ascending bool) string {
	if !c.Sortable || c.SortKey != sortKey {
		return c.Title
	}
	if ascending {
		return c.Title + " ↑"
	}
	return c.Title + " ↓"
}

// Row is one target in the host table: its metrics snapshot and the
// plain-text cells, one per column of the TableData
type Row struct {
	Metric stats.Metrics
	Cells  []string
}

// TableData represents table data optimized for tview.Table with go-pretty fallback
type TableData struct {
	Columns []Column
	Headers []string
	Rows    []Row
}

// NewTableData creates TableData from metrics
func NewTableData(metrics []stats.Metrics, sortKey stats.Key, ascending bool) *TableData {
	headers := make([]string, len(Columns))
	for i, c := range Columns {
		headers[i] = c.Header(sortKey, ascending)
	}

	rows := make([]Row, len(metrics))
	for i, m := range metrics {
		cells := make([]string, len(Columns))
		for j, c := range Columns {
			cells[j] = c.Value(m)
		}
		rows[i] = Row{Metric: m, Cells: cells}
	}

	return &TableData{
		Columns: Columns,
		Headers: headers,
		Rows:    rows,
	}
}

// ColumnIndex returns the position of the column with the given ID, or -1
func (td *TableData) ColumnIndex(id ColumnID) int {
	for i, c := range td.Columns {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// ToGoPrettyTable converts to go-pretty table format for final output only
func (td *TableData) ToGoPrettyTable() table.Writer {
	text.OverrideRuneWidthEastAsianWidth(false)
	t := table.NewWriter()

	// Convert headers to interface{} slice
	headerRow := make(table.Row, len(td.Headers))
	for i, h := range td.Headers {
		headerRow[i] = h
	}
	t.AppendHeader(headerRow)

	// Add rows
	for _, row := range td.Rows {
		rowData := make(table.Row, len(row.Cells))
		for i, cell := range row.Cells {
			rowData[i] = cell
		}
		t.AppendRow(rowData)
	}

	return t
}

// GetMetricAtRow returns the metric for a given row index
func (td *TableData) GetMetricAtRow(row int) (stats.Metrics, bool) {
	if row < 0 || row >= len(td.Rows) {
		return nil, false
	}
	return td.Rows[row].Metric, true
}
