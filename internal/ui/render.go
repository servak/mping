package ui

import (
	"fmt"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/servak/mping/internal/stats"
)

// Renderer はコンテンツ生成を担当
type Renderer struct {
	mm       *stats.MetricsManager
	config   *Config
	interval time.Duration
	sortKey  stats.Key
}

// NewRenderer は新しい Renderer を作成
func NewRenderer(mm *stats.MetricsManager, cfg *Config, interval time.Duration) *Renderer {
	return &Renderer{
		mm:       mm,
		config:   cfg,
		interval: interval,
		sortKey:  stats.Success,
	}
}

// SetSortKey はソートキーを設定
func (r *Renderer) SetSortKey(key stats.Key) {
	r.sortKey = key
}

// RenderHeader はヘッダーテキストを生成
func (r *Renderer) RenderHeader() string {
	if r.config.EnableColors && r.config.Colors.Header != "" {
		sortText := fmt.Sprintf("[%s]Sort: %s[-]", r.config.Colors.Header, r.sortKey)
		intervalText := fmt.Sprintf("[%s]Interval: %dms[-]", r.config.Colors.Header, r.interval.Milliseconds())
		titleText := fmt.Sprintf("[%s]%s[-]", r.config.Colors.Header, r.config.Title)
		return fmt.Sprintf("%s    %s    %s", sortText, intervalText, titleText)
	} else {
		return fmt.Sprintf("Sort: %s    Interval: %dms    %s", r.sortKey, r.interval.Milliseconds(), r.config.Title)
	}
}

// RenderMain はメインコンテンツ（テーブル）を生成
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

// RenderFooter はフッターテキストを生成
func (r *Renderer) RenderFooter() string {
	if r.config.EnableColors && r.config.Colors.Footer != "" {
		helpText := fmt.Sprintf("[%s]h:help[-]", r.config.Colors.Footer)
		quitText := fmt.Sprintf("[%s]q:quit[-]", r.config.Colors.Footer)
		sortText := fmt.Sprintf("[%s]s:sort[-]", r.config.Colors.Footer)
		resetText := fmt.Sprintf("[%s]R:reset[-]", r.config.Colors.Footer)
		moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", r.config.Colors.Footer)
		return fmt.Sprintf("%s  %s  %s  %s  %s", helpText, quitText, sortText, resetText, moveText)
	} else {
		return "h:help  q:quit  s:sort  R:reset  j/k/g/G/u/d:move"
	}
}

// renderTable はテーブルを生成（table.goから移動）
func (r *Renderer) renderTable() table.Writer {
	t := table.NewWriter()
	t.AppendHeader(table.Row{stats.Host, stats.Sent, stats.Success, stats.Fail, stats.Loss, stats.Last, stats.Avg, stats.Best, stats.Worst, stats.LastSuccTime, stats.LastFailTime, "FAIL Reason"})
	df := DurationFormater
	tf := TimeFormater
	for _, m := range r.mm.GetSortedMetricsByKey(r.sortKey) {
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

// TableRender は外部から使用されるテーブル生成関数
func TableRender(mm *stats.MetricsManager, key stats.Key) table.Writer {
	t := table.NewWriter()
	t.AppendHeader(table.Row{stats.Host, stats.Sent, stats.Success, stats.Fail, stats.Loss, stats.Last, stats.Avg, stats.Best, stats.Worst, stats.LastSuccTime, stats.LastFailTime, "FAIL Reason"})
	df := DurationFormater
	tf := TimeFormater
	for _, m := range mm.GetSortedMetricsByKey(key) {
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