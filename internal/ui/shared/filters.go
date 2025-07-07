package shared

import (
	"strings"

	"github.com/servak/mping/internal/stats"
)

// FilterMetrics filters metrics based on filter text
func FilterMetrics(metrics []stats.MetricsReader, filterText string) []stats.MetricsReader {
	if filterText == "" {
		return metrics
	}
	
	filtered := []stats.MetricsReader{}
	filterLower := strings.ToLower(filterText)
	for _, m := range metrics {
		if strings.Contains(strings.ToLower(m.GetName()), filterLower) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}