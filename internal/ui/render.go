package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/servak/mping/internal/stats"
)

// Renderer handles content generation
type Renderer struct {
	mm         *stats.MetricsManager
	config     *Config
	interval   time.Duration
	timeout    time.Duration
	sortKey    stats.Key
	ascending  bool
	filterText string
}

// NewRenderer creates a new Renderer instance
func NewRenderer(mm *stats.MetricsManager, cfg *Config, interval, timeout time.Duration) *Renderer {
	// Fix Unicode character width calculation issues in East Asian locales
	text.OverrideRuneWidthEastAsianWidth(false)

	return &Renderer{
		mm:         mm,
		config:     cfg,
		interval:   interval,
		timeout:    timeout,
		sortKey:    stats.Success,
		ascending:  true,
		filterText: "",
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
	
	var parts []string
	if r.config.EnableColors && r.config.Colors.Header != "" {
		parts = append(parts, fmt.Sprintf("[%s]Sort: %s[-]", r.config.Colors.Header, sortDisplay))
		parts = append(parts, fmt.Sprintf("[%s]Interval: %dms[-]", r.config.Colors.Header, r.interval.Milliseconds()))
		parts = append(parts, fmt.Sprintf("[%s]Timeout: %dms[-]", r.config.Colors.Header, r.timeout.Milliseconds()))
		
		if r.filterText != "" {
			parts = append(parts, fmt.Sprintf("[%s]Filter: %s[-]", r.config.Colors.Warning, r.filterText))
		}
		
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", r.config.Colors.Header, r.config.Title))
	} else {
		parts = append(parts, fmt.Sprintf("Sort: %s", sortDisplay))
		parts = append(parts, fmt.Sprintf("Interval: %dms", r.interval.Milliseconds()))
		parts = append(parts, fmt.Sprintf("Timeout: %dms", r.timeout.Milliseconds()))
		
		if r.filterText != "" {
			parts = append(parts, fmt.Sprintf("Filter: %s", r.filterText))
		}
		
		parts = append(parts, r.config.Title)
	}
	
	return strings.Join(parts, "    ")
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
		filterText := fmt.Sprintf("[%s]/:filter[-]", r.config.Colors.Footer)
		moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", r.config.Colors.Footer)
		return fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s", helpText, quitText, sortText, reverseText, resetText, filterText, moveText)
	} else {
		return "h:help  q:quit  s:sort  r:reverse  R:reset  /:filter  j/k/g/G/u/d:move"
	}
}

// ReverseSort toggles between ascending/descending for current sort key
func (r *Renderer) ReverseSort() {
	r.ascending = !r.ascending
}

// SetFilter sets the filter text
func (r *Renderer) SetFilter(filter string) {
	r.filterText = filter
}

// GetFilter returns the current filter text
func (r *Renderer) GetFilter() string {
	return r.filterText
}

// ClearFilter clears the filter text
func (r *Renderer) ClearFilter() {
	r.filterText = ""
}

// getFilteredMetrics returns filtered metrics based on filter text
func (r *Renderer) getFilteredMetrics() []stats.Metrics {
	metrics := r.mm.SortBy(r.sortKey, r.ascending)
	if r.filterText == "" {
		return metrics
	}
	
	filtered := []stats.Metrics{}
	filterLower := strings.ToLower(r.filterText)
	for _, m := range metrics {
		if strings.Contains(strings.ToLower(m.Name), filterLower) {
			filtered = append(filtered, m)
		}
	}
	return filtered
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
	for _, m := range r.getFilteredMetrics() {
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
