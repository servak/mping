package ui

import (
	"fmt"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/servak/mping/internal/stats"
)

// Renderer handles content generation
type Renderer struct {
	mm        *stats.MetricsManager
	config    *Config
	interval  time.Duration
	timeout   time.Duration
	sortKey   stats.Key
	ascending bool
}

// NewRenderer creates a new Renderer instance
func NewRenderer(mm *stats.MetricsManager, cfg *Config, interval, timeout time.Duration) *Renderer {
	// Fix Unicode character width calculation issues in East Asian locales
	text.OverrideRuneWidthEastAsianWidth(false)

	return &Renderer{
		mm:        mm,
		config:    cfg,
		interval:  interval,
		timeout:   timeout,
		sortKey:   stats.Success,
		ascending: true,
	}
}

// SetSortKey sets the sort key
func (r *Renderer) SetSortKey(key stats.Key) {
	// New key always resets to ascending order
	r.sortKey = key
	r.ascending = true
}

// RenderHeader generates header text
func (r *Renderer) RenderHeader() string {
	sortDisplay := r.sortKey.String()
	if r.config.EnableColors && r.config.Colors.Header != "" {
		sortText := fmt.Sprintf("[%s]Sort: %s[-]", r.config.Colors.Header, sortDisplay)
		intervalText := fmt.Sprintf("[%s]Interval: %dms[-]", r.config.Colors.Header, r.interval.Milliseconds())
		timeoutText := fmt.Sprintf("[%s]Timeout: %dms[-]", r.config.Colors.Header, r.timeout.Milliseconds())
		titleText := fmt.Sprintf("[%s]%s[-]", r.config.Colors.Header, r.config.Title)
		return fmt.Sprintf("%s    %s    %s    %s", sortText, intervalText, timeoutText, titleText)
	} else {
		return fmt.Sprintf("Sort: %s    Interval: %dms    Timeout: %dms    %s", sortDisplay, r.interval.Milliseconds(), r.timeout.Milliseconds(), r.config.Title)
	}
}

// RenderMain generates main content (table)
func (r *Renderer) RenderMain() string {
	t := r.renderTable()
	if r.config.Border {
		t.SetStyle(table.StyleLight)
	} else {
		t.SetStyle(table.Style{
			Box: table.StyleBoxLight,
			Options: table.Options{
				DrawBorder:      false,
				SeparateColumns: false,
			},
		})
	}
	return t.Render()
}

// RenderFooter generates footer text
func (r *Renderer) RenderFooter() string {
	if r.config.EnableColors && r.config.Colors.Footer != "" {
		helpText := fmt.Sprintf("[%s]h:help[-]", r.config.Colors.Footer)
		quitText := fmt.Sprintf("[%s]q:quit[-]", r.config.Colors.Footer)
		sortText := fmt.Sprintf("[%s]s:sort[-]", r.config.Colors.Footer)
		reverseText := fmt.Sprintf("[%s]r:reverse[-]", r.config.Colors.Footer)
		resetText := fmt.Sprintf("[%s]R:reset[-]", r.config.Colors.Footer)
		moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", r.config.Colors.Footer)
		return fmt.Sprintf("%s  %s  %s  %s  %s  %s", helpText, quitText, sortText, reverseText, resetText, moveText)
	} else {
		return "h:help  q:quit  s:sort  r:reverse  R:reset  j/k/g/G/u/d:move"
	}
}

// ReverseSort toggles between ascending/descending for current sort key
func (r *Renderer) ReverseSort() {
	r.ascending = !r.ascending
}

// headerWithArrow generates header with sort direction arrow
func (r *Renderer) headerWithArrow(key stats.Key) string {
	header := key.String()
	if key == r.sortKey {
		if r.ascending {
			return header + " ↑" // Unicode arrow
		} else {
			return header + " ↓" // Unicode arrow
		}
	}
	return header
}

// renderTable generates the table (moved from table.go)
func (r *Renderer) renderTable() table.Writer {
	t := table.NewWriter()

	// Generate dynamic headers with sort direction arrows
	headers := []interface{}{
		r.headerWithArrow(stats.Host),
		r.headerWithArrow(stats.Sent),
		r.headerWithArrow(stats.Success),
		r.headerWithArrow(stats.Fail),
		r.headerWithArrow(stats.Loss),
		r.headerWithArrow(stats.Last),
		r.headerWithArrow(stats.Avg),
		r.headerWithArrow(stats.Best),
		r.headerWithArrow(stats.Worst),
		r.headerWithArrow(stats.LastSuccTime),
		r.headerWithArrow(stats.LastFailTime),
		"FAIL Reason", // Last column is not sortable
	}
	t.AppendHeader(table.Row(headers))
	df := DurationFormater
	tf := TimeFormater
	for _, m := range r.mm.SortBy(r.sortKey, r.ascending) {
		t.AppendRow(table.Row{
			m.Name,
			m.Total,
			m.Successful,
			m.Failed,
			fmt.Sprintf("%5.1f%%", m.Loss),
			df(m.LastRTT),
			df(m.AverageRTT),
			df(m.MinimumRTT),
			df(m.MaximumRTT),
			tf(m.LastSuccTime),
			tf(m.LastFailTime),
			m.LastFailDetail,
		})
	}
	return t
}

// TableRender is a table generation function for external use
func TableRender(mm *stats.MetricsManager, key stats.Key) table.Writer {
	t := table.NewWriter()
	t.AppendHeader(table.Row{stats.Host, stats.Sent, stats.Success, stats.Fail, stats.Loss, stats.Last, stats.Avg, stats.Best, stats.Worst, stats.LastSuccTime, stats.LastFailTime, "FAIL Reason"})
	df := DurationFormater
	tf := TimeFormater
	for _, m := range mm.SortBy(key, true) { // Default ascending order
		t.AppendRow(table.Row{
			m.Name,
			m.Total,
			m.Successful,
			m.Failed,
			fmt.Sprintf("%5.1f%%", m.Loss),
			df(m.LastRTT),
			df(m.AverageRTT),
			df(m.MinimumRTT),
			df(m.MaximumRTT),
			tf(m.LastSuccTime),
			tf(m.LastFailTime),
			m.LastFailDetail,
		})
	}
	return t
}
