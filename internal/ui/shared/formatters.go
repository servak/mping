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
	// Color-coded basic statistics
	lossRate := metric.GetLoss()
	lossColor := "green"
	if lossRate > 50 {
		lossColor = "red"
	} else if lossRate > 10 {
		lossColor = "yellow"
	}
	
	successColor := "green"
	if metric.GetSuccessful() == 0 {
		successColor = "red"
	}
	
	failColor := "white"
	if metric.GetFailed() > 0 {
		failColor = "red"
	}
	
	basicInfo := fmt.Sprintf(`[cyan]Total Probes:[white] %d
[%s]Successful:[white] %d
[%s]Failed:[white] %d
[cyan]Loss Rate:[white] [%s]%.1f%%[white]
[cyan]Last RTT:[white] %s
[cyan]Average RTT:[white] %s
[cyan]Minimum RTT:[white] %s
[cyan]Maximum RTT:[white] %s
[cyan]Last Success:[white] %s
[cyan]Last Failure:[white] %s
[cyan]Last Error:[white] %s`,
		metric.GetTotal(),
		successColor, metric.GetSuccessful(),
		failColor, metric.GetFailed(),
		lossColor, lossRate,
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
	sb.WriteString("\n[yellow]Recent History (last 10 entries):[white]\n")
	sb.WriteString("[cyan]Time     Status RTT     Details[white]\n")
	sb.WriteString("[gray]-------- ------ ------- --------[white]\n")

	for _, entry := range history {
		statusColor := "green"
		status := "OK"
		details := ""
		
		if !entry.Success {
			status = "FAIL"
			statusColor = "red"
			// Show error message for failed entries
			if entry.Error != "" {
				details = fmt.Sprintf("[red]%s[white]", entry.Error)
			}
		} else {
			// Show probe-specific details for successful entries
			details = formatProbeDetails(entry.Details)
		}

		sb.WriteString(fmt.Sprintf("[gray]%-8s[white] [%s]%-6s[white] %-7s %s\n",
			entry.Timestamp.Format("15:04:05"),
			statusColor, status,
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
	case "icmp", "icmpv4", "icmpv6":
		if details.ICMP != nil {
			// Only show sequence and size for now (TTL is not properly implemented)
			return fmt.Sprintf("seq=%d size=%d", 
				details.ICMP.Sequence, details.ICMP.DataSize)
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