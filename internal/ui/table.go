package ui

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/servak/mping/internal/stats"
)

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
