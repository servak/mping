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
func FormatHostDetail(metric stats.Metrics, theme *Theme) string {
	// Color-coded basic statistics
	lossRate := metric.GetLoss()
	lossColor := theme.Success
	if lossRate > 50 {
		lossColor = theme.Error
	} else if lossRate > 10 {
		lossColor = theme.Warning
	}

	successColor := theme.Success
	if metric.GetSuccessful() == 0 {
		successColor = theme.Error
	}

	failColor := theme.Primary
	if metric.GetFailed() > 0 {
		failColor = theme.Error
	}

	basicInfo := fmt.Sprintf(`[%s]Total Probes:[%s] %d
[%s]Successful:[%s] %d
[%s]Failed:[%s] %d
[%s]Loss Rate:[%s] [%s]%.1f%%[%s]
[%s]Last RTT:[%s] %s
[%s]Average RTT:[%s] %s
[%s]Minimum RTT:[%s] %s
[%s]Maximum RTT:[%s] %s
[%s]Last Success:[%s] %s
[%s]Last Failure:[%s] %s
[%s]Last Error:[%s] %s`,
		theme.Accent, theme.Primary, metric.GetTotal(),
		successColor, theme.Primary, metric.GetSuccessful(),
		failColor, theme.Primary, metric.GetFailed(),
		theme.Accent, theme.Primary, lossColor, lossRate, theme.Primary,
		theme.Accent, theme.Primary, DurationFormater(metric.GetLastRTT()),
		theme.Accent, theme.Primary, DurationFormater(metric.GetAverageRTT()),
		theme.Accent, theme.Primary, DurationFormater(metric.GetMinimumRTT()),
		theme.Accent, theme.Primary, DurationFormater(metric.GetMaximumRTT()),
		theme.Accent, theme.Primary, TimeFormater(metric.GetLastSuccTime()),
		theme.Accent, theme.Primary, TimeFormater(metric.GetLastFailTime()),
		theme.Accent, theme.Primary, metric.GetLastFailDetail(),
	)

	// Add history section
	historySection := FormatHistory(metric, theme)
	if historySection != "" {
		basicInfo += "\n\n" + historySection
	}

	return basicInfo
}

// FormatHistory generates history section for a host
func FormatHistory(metric stats.Metrics, theme *Theme) string {
	history := metric.GetRecentHistory(10)
	if len(history) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n[%s]Recent History (last 10 entries):[%s]\n", theme.Warning, theme.Primary))
	sb.WriteString(fmt.Sprintf("[%s]Time     Status RTT     Details[%s]\n", theme.Accent, theme.Primary))
	sb.WriteString(fmt.Sprintf("[%s]-------- ------ ------- --------[%s]\n", theme.Separator, theme.Primary))

	for _, entry := range history {
		statusColor := theme.Success
		status := "OK"
		details := ""

		if !entry.Success {
			status = "FAIL"
			statusColor = theme.Error
			// Show error message for failed entries
			if entry.Error != "" {
				details = fmt.Sprintf("[%s]%s[%s]", theme.Error, entry.Error, theme.Primary)
			}
		} else {
			// Show probe-specific details for successful entries
			details = formatProbeDetails(entry.Details)
		}

		sb.WriteString(fmt.Sprintf("[%s]%-8s[%s] [%s]%-6s[%s] %-7s %s\n",
			theme.Timestamp, entry.Timestamp.Format("15:04:05"),
			theme.Primary, statusColor, status,
			theme.Primary, DurationFormater(entry.RTT),
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
	case "icmp", "icmpv4", "icmpv6":
		if details.ICMP != nil {
			// Show enhanced ICMP details
			var parts []string
			parts = append(parts, fmt.Sprintf("seq=%d", details.ICMP.Sequence))
			parts = append(parts, fmt.Sprintf("size=%d", details.ICMP.PacketSize))

			if details.ICMP.ICMPType >= 0 {
				parts = append(parts, fmt.Sprintf("type=%d", details.ICMP.ICMPType))
			}

			if details.ICMP.Payload != "" {
				parts = append(parts, fmt.Sprintf("payload=%s", details.ICMP.Payload))
			}

			return strings.Join(parts, " ")
		}
		return "icmp ping"
	case "http", "https":
		if details.HTTP != nil {
			return fmt.Sprintf("status=%d size=%d",
				details.HTTP.StatusCode, details.HTTP.ResponseSize)
		}
		return "http probe"
	case "dns":
		if details.DNS != nil {
			proto := ""
			if details.DNS.UseTCP {
				proto = "tcp "
			}

			// Show just the essential info: protocol, response code, answer count, and first answer
			baseInfo := fmt.Sprintf("%scode=%d ans=%d",
				proto, details.DNS.ResponseCode, details.DNS.AnswerCount)

			// Add first answer if available
			if len(details.DNS.Answers) > 0 {
				firstAnswer := extractDNSAnswer(details.DNS.Answers[0])
				if firstAnswer != "" {
					baseInfo += " " + firstAnswer
				}
			}

			return baseInfo
		}
		return "dns query"
	case "ntp":
		if details.NTP != nil {
			offset := time.Duration(details.NTP.Offset) * time.Microsecond
			return fmt.Sprintf("stratum=%d offset=%s",
				details.NTP.Stratum, DurationFormater(offset))
		}
		return "ntp sync"
	case "tcp":
		return "connection"
	}

	return ""
}

// extractDNSAnswer extracts the answer value from DNS record string
// Example: "google.com. 300 IN A 142.250.196.14" -> "142.250.196.14"
func extractDNSAnswer(record string) string {
	if record == "" {
		return ""
	}

	// Split by whitespace and get the last part (the answer value)
	parts := strings.Fields(record)
	if len(parts) == 0 {
		return ""
	}

	// The last part is usually the answer value
	answer := parts[len(parts)-1]

	// Truncate very long answers (like long TXT records)
	// TODO: Make this configurable in the future
	maxAnswerLength := 35
	if len(answer) > maxAnswerLength {
		answer = answer[:maxAnswerLength-3] + "..."
	}

	return answer
}
