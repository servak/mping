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
func FormatHostDetail(metric stats.MetricsReader) string {
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
		metric.GetName(),
		metric.GetTotal(),
		metric.GetSuccessful(),
		metric.GetFailed(),
		metric.GetLoss(),
		DurationFormater(metric.GetLastRTT()),
		DurationFormater(metric.GetAverageRTT()),
		DurationFormater(metric.GetMinimumRTT()),
		DurationFormater(metric.GetMaximumRTT()),
		TimeFormater(metric.GetLastSuccTime()),
		TimeFormater(metric.GetLastFailTime()),
		metric.GetLastFailDetail(),
	)
}