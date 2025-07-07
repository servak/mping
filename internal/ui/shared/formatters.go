package shared

import (
	"fmt"
	"strings"
	"time"

	"github.com/servak/mping/internal/prober"
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
	basicInfo := fmt.Sprintf(`Host Details: %s

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

	// Add history section
	historySection := FormatHistory(metric)
	if historySection != "" {
		basicInfo += "\n\n" + historySection
	}

	return basicInfo
}

// FormatHistory generates history section for a host
func FormatHistory(metric stats.MetricsReader) string {
	history := metric.GetRecentHistory(10)
	if len(history) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Recent History (last 10 entries):\n")
	sb.WriteString("Time     Status RTT     Details\n")
	sb.WriteString("-------- ------ ------- --------\n")

	for _, entry := range history {
		status := "OK"
		if !entry.Success {
			status = "FAIL"
		}

		details := formatProbeDetails(entry.Details)
		sb.WriteString(fmt.Sprintf("%-8s %-6s %-7s %s\n",
			entry.Timestamp.Format("15:04:05"),
			status,
			DurationFormater(entry.RTT),
			details,
		))
	}

	return sb.String()
}

// formatProbeDetails formats probe-specific details
func formatProbeDetails(details *prober.ProbeDetails) string {
	if details == nil {
		return ""
	}

	switch details.ProbeType {
	case "icmp":
		if details.ICMP != nil {
			return fmt.Sprintf("seq=%d ttl=%d size=%d", 
				details.ICMP.Sequence, details.ICMP.TTL, details.ICMP.DataSize)
		}
	case "http", "https":
		if details.HTTP != nil {
			return fmt.Sprintf("status=%d size=%d", 
				details.HTTP.StatusCode, details.HTTP.ResponseSize)
		}
	case "dns":
		if details.DNS != nil {
			return fmt.Sprintf("code=%d answers=%d server=%s", 
				details.DNS.ResponseCode, details.DNS.AnswerCount, details.DNS.Server)
		}
	case "ntp":
		if details.NTP != nil {
			offset := time.Duration(details.NTP.Offset) * time.Microsecond
			return fmt.Sprintf("stratum=%d offset=%s", 
				details.NTP.Stratum, DurationFormater(offset))
		}
	case "tcp":
		return "connection"
	}
	
	return ""
}