package shared

import (
	"fmt"
	"time"

	"github.com/servak/mping/internal/stats"
)

func DurationFormater(duration time.Duration) string {
	if duration == 0 {
		return "-"
	} else if duration.Microseconds() < 1000 {
		return fmt.Sprintf("%3dµs", duration.Microseconds())
	} else if duration.Milliseconds() < 1000 {
		return fmt.Sprintf("%3dms", duration.Milliseconds())
	} else {
		return fmt.Sprintf("%3.0fs", duration.Seconds())
	}
}

func TimeFormater(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("15:04:05")
}

// FormatHostDetail generates detailed information for a host
func FormatHostDetail(metric stats.Metrics) string {
	return fmt.Sprintf(`Host Details: %s

Total Probes: %d
Successful: %d
Failed: %d
Loss Rate: %.1f%%
Last RTT: %s
Average RTT: %s
Minimum RTT: %s
Maximum RTT: %s
Last Success: %s
Last Failure: %s
Last Error: %s`,
		metric.Name,
		metric.Total,
		metric.Successful,
		metric.Failed,
		metric.Loss,
		DurationFormater(metric.LastRTT),
		DurationFormater(metric.AverageRTT),
		DurationFormater(metric.MinimumRTT),
		DurationFormater(metric.MaximumRTT),
		TimeFormater(metric.LastSuccTime),
		TimeFormater(metric.LastFailTime),
		metric.LastFailDetail,
	)
}