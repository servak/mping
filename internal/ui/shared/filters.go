package shared

import (
	"strings"

	"github.com/servak/mping/internal/stats"
)

// FilterMetrics filters metrics based on filter text
func FilterMetrics(metrics []stats.Metrics, filterText string) []stats.Metrics {
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