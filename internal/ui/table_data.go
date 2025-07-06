package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

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
			m.Name,
			fmt.Sprintf("%d", m.Total),
			fmt.Sprintf("%d", m.Successful),
			fmt.Sprintf("%d", m.Failed),
			fmt.Sprintf("%5.1f%%", m.Loss),
			df(m.LastRTT),
			df(m.AverageRTT),
			df(m.MinimumRTT),
			df(m.MaximumRTT),
			tf(m.LastSuccTime),
			tf(m.LastFailTime),
			m.LastFailDetail,
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

// ToTviewTable converts to interactive tview.Table format (primary UI)
func (td *TableData) ToTviewTable() *tview.Table {
	t := tview.NewTable().
		SetFixed(1, 0).
		SetSelectable(true, false).
		SetBorders(false).  // Disable all borders
		SetSeparator(' ').  // Use space separator instead of lines
		SetSelectedStyle(tcell.StyleDefault.
			Background(tcell.ColorDarkBlue).
			Foreground(tcell.ColorWhite))  // Pattern 1: DarkBlue + White - k9s style

	// Define alignment for each column
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

	// Set headers with direct TableCell struct
	for col, header := range td.Headers {
		alignment := tview.AlignLeft
		if col < len(alignments) {
			alignment = alignments[col]
		}
		
		t.SetCell(0, col, &tview.TableCell{
			Text:          "  " + header + "  ",
			Color:         tcell.ColorYellow,
			Align:         alignment,
			NotSelectable: true,
		})
	}

	// Set rows with direct TableCell struct
	for row, rowData := range td.Rows {
		for col, cellData := range rowData {
			alignment := tview.AlignLeft
			if col < len(alignments) {
				alignment = alignments[col]
			}
			
			t.SetCell(row+1, col, &tview.TableCell{
				Text:  "  " + cellData + "  ",
				Color: tcell.ColorWhite,
				Align: alignment,
			})
		}
	}

	return t
}

// GetMetricAtRow returns the metric for a given row index
func (td *TableData) GetMetricAtRow(row int) (stats.Metrics, bool) {
	if row < 0 || row >= len(td.Metrics) {
		return stats.Metrics{}, false
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
