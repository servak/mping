package shared

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/servak/mping/internal/stats"
)

// ColumnLastFailTime is the index of the LastFailTime column in TableData
const ColumnLastFailTime = 10

// TableData represents table data optimized for tview.Table with go-pretty fallback
type TableData struct {
	Headers []string
	Rows    [][]string
	Metrics []stats.Metrics // Keep reference for interactive row selection
}

// NewTableData creates TableData from metrics
func NewTableData(metrics []stats.Metrics, sortKey stats.Key, ascending bool) *TableData {
	// Generate headers with sort arrows
	headers := []string{
		headerWithArrow("Host", stats.Host, sortKey, ascending),
		headerWithArrow("Sent", stats.Sent, sortKey, ascending),
		headerWithArrow("Succ", stats.Success, sortKey, ascending),
		headerWithArrow("Fail", stats.Fail, sortKey, ascending),
		headerWithArrow("Loss", stats.Loss, sortKey, ascending),
		headerWithArrow("Last", stats.Last, sortKey, ascending),
		headerWithArrow("Avg", stats.Avg, sortKey, ascending),
		headerWithArrow("Best", stats.Best, sortKey, ascending),
		headerWithArrow("Worst", stats.Worst, sortKey, ascending),
		headerWithArrow("LastSuccTime", stats.LastSuccTime, sortKey, ascending),
		headerWithArrow("LastFailTime", stats.LastFailTime, sortKey, ascending),
		"FAIL Reason",
	}

	// Generate rows
	rows := make([][]string, len(metrics))
	df := DurationFormater
	tf := TimeFormater

	for i, m := range metrics {
		rows[i] = []string{
			m.GetName(),
			fmt.Sprintf("%d", m.GetTotal()),
			fmt.Sprintf("%d", m.GetSuccessful()),
			fmt.Sprintf("%d", m.GetFailed()),
			fmt.Sprintf("%5.1f%%", m.GetLoss()),
			df(m.GetLastRTT()),
			df(m.GetAverageRTT()),
			df(m.GetMinimumRTT()),
			df(m.GetMaximumRTT()),
			tf(m.GetLastSuccTime()),
			tf(m.GetLastFailTime()),
			m.GetLastFailDetail(),
		}
	}

	return &TableData{
		Headers: headers,
		Rows:    rows,
		Metrics: metrics,
	}
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
		rowData := make(table.Row, len(row))
		for i, cell := range row {
			rowData[i] = cell
		}
		t.AppendRow(rowData)
	}

	return t
}

// GetMetricAtRow returns the metric for a given row index
func (td *TableData) GetMetricAtRow(row int) (stats.Metrics, bool) {
	if row < 0 || row >= len(td.Metrics) {
		return nil, false
	}
	return td.Metrics[row], true
}

// headerWithArrow generates header with sort direction arrow
func headerWithArrow(header string, key stats.Key, sortKey stats.Key, ascending bool) string {
	if key == sortKey {
		if ascending {
			return header + " ↑"
		} else {
			return header + " ↓"
		}
	}
	return header
}
