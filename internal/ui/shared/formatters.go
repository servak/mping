package shared

import (
	"fmt"
	"slices"
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

// FormatClock formats a time with millisecond precision for outage timelines
func FormatClock(t time.Time) string {
	return t.Format("15:04:05.000")
}

// FormatOutageDuration formats an outage length with millisecond precision
func FormatOutageDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
	return d.Round(time.Millisecond).String()
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

	pct := metric.GetRTTPercentiles(50, 95, 99)

	basicInfo := fmt.Sprintf(`[%s]Total Probes:[%s] %d
[%s]Successful:[%s] %d
[%s]Failed:[%s] %d
[%s]Loss Rate:[%s] [%s]%.1f%%[%s]
[%s]Last RTT:[%s] %s
[%s]Average RTT:[%s] %s
[%s]Minimum RTT:[%s] %s
[%s]Maximum RTT:[%s] %s
[%s]Jitter (stddev):[%s] %s
[%s]RTT p50/p95/p99:[%s] %s / %s / %s
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
		theme.Accent, theme.Primary, DurationFormater(metric.GetJitter()),
		theme.Accent, theme.Primary, DurationFormater(pct[0]), DurationFormater(pct[1]), DurationFormater(pct[2]),
		theme.Accent, theme.Primary, TimeFormater(metric.GetLastSuccTime()),
		theme.Accent, theme.Primary, TimeFormater(metric.GetLastFailTime()),
		theme.Accent, theme.Primary, metric.GetLastFailDetail(),
	)

	basicInfo += "\n\n" + FormatOutages(metric, theme)

	// Add history section
	historySection := FormatHistory(metric, theme)
	if historySection != "" {
		basicInfo += "\n\n" + historySection
	}

	return basicInfo
}

// maxDetailOutages is the number of recent outages listed in host details
const maxDetailOutages = 5

// FormatOutages generates the outage section for a host
func FormatOutages(metric stats.Metrics, theme *Theme) string {
	summary := metric.GetOutageSummary()
	if summary.Count == 0 {
		return fmt.Sprintf("[%s]Outages:[%s] none", theme.Accent, theme.Primary)
	}

	var sb strings.Builder
	state := ""
	if summary.Ongoing {
		state = fmt.Sprintf(" [%s]DOWN[%s]", theme.Error, theme.Primary)
	}
	fmt.Fprintf(&sb, "[%s]Outages:[%s] %d (total %s, longest %s)%s\n",
		theme.Accent, theme.Primary, summary.Count,
		FormatOutageDuration(summary.Total), FormatOutageDuration(summary.Longest), state)
	fmt.Fprintf(&sb, "[%s]Start        End          Duration  Lost[%s]\n", theme.Accent, theme.Primary)

	now := time.Now()
	outages := metric.GetOutages()
	for i := len(outages) - 1; i >= 0 && i >= len(outages)-maxDetailOutages; i-- {
		o := outages[i]
		end := "(ongoing)   "
		color := theme.Primary
		if o.Ongoing() {
			color = theme.Error
		} else {
			end = FormatClock(o.End)
		}
		fmt.Fprintf(&sb, "[%s]%s[%s] [%s]%s %9s %5d[%s]\n",
			theme.Timestamp, FormatClock(o.Start), theme.Primary,
			color, end, FormatOutageDuration(o.Duration(now)), o.LostProbes, theme.Primary)
	}
	if len(outages) > maxDetailOutages {
		fmt.Fprintf(&sb, "[%s]... %d more[%s]\n", theme.Secondary, len(outages)-maxDetailOutages, theme.Primary)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// FormatHistory generates history section for a host
func FormatHistory(metric stats.Metrics, theme *Theme) string {
	history := metric.GetRecentHistory(10)
	if len(history) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n[%s]Recent History (last 10 entries):[%s]\n", theme.Warning, theme.Primary)
	fmt.Fprintf(&sb, "[%s]Time     Status RTT     Details[%s]\n", theme.Accent, theme.Primary)
	fmt.Fprintf(&sb, "[%s]-------- ------ ------- --------[%s]\n", theme.Separator, theme.Primary)

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

		fmt.Fprintf(&sb, "[%s]%-8s[%s] [%s]%-6s[%s] %-7s %s\n",
			theme.Timestamp, entry.Timestamp.Format("15:04:05"),
			theme.Primary, statusColor, status,
			theme.Primary, DurationFormater(entry.RTT),
			details,
		)
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

// FormatLastFailCell renders the LastFailTime cell for the host list with the
// duration of the outage the last failure belongs to (tview color tags):
//
//	"16:44:20 (DOWN 5.20s)"  red, while the outage is ongoing
//	"16:44:15 (0.50s)"       yellow, after it recovered
//	"16:44:15"               a failure not yet applied to outage detection
//	"-"                      no failure
func FormatLastFailCell(m stats.Metrics, now time.Time, theme *Theme) string {
	lastFail := m.GetLastFailTime()
	if lastFail.IsZero() {
		return "-"
	}
	clock := TimeFormater(lastFail)
	last, ok := m.GetLastOutage()
	if !ok {
		return clock
	}
	switch {
	case last.Ongoing():
		return fmt.Sprintf("[%s]%s (DOWN %s)[-]", theme.Error, clock, FormatShortDuration(last.Duration(now)))
	case !lastFail.Before(last.Start) && lastFail.Before(last.End):
		return fmt.Sprintf("[%s]%s (%s)[-]", theme.Warning, clock, FormatShortDuration(last.Duration(now)))
	}
	return clock
}

// FormatShortDuration formats a duration compactly for table cells
func FormatShortDuration(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// HistoryWidth is the number of recent probes shown in the host list
const HistoryWidth = 20

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// FormatSparkline renders the newest `width` probe results (history is newest
// first, as returned by GetRecentHistory) as an oldest-to-newest strip with
// tview color tags. Bar height shows how many milliseconds an RTT exceeds the
// median of the whole given history (see sparkLevel), so a host's normal
// jitter stays flat regardless of its baseline; failures are shown as '×'.
func FormatSparkline(history []stats.HistoryEntry, width int, theme *Theme) string {
	if width <= 0 {
		return ""
	}

	var rtts []time.Duration
	for _, e := range history {
		if e.Success {
			rtts = append(rtts, e.RTT)
		}
	}
	var median time.Duration
	if len(rtts) > 0 {
		slices.Sort(rtts)
		median = rtts[(len(rtts)-1)/2]
	}
	if len(history) > width {
		history = history[:width]
	}

	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", width-len(history)))
	lastColor := ""
	setColor := func(c string) {
		if c != lastColor {
			fmt.Fprintf(&sb, "[%s]", c)
			lastColor = c
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		e := history[i]
		if !e.Success {
			setColor(theme.Error)
			sb.WriteRune('×')
			continue
		}
		level := sparkLevel(e.RTT, median)
		if level >= sparkWarnLevel {
			setColor(theme.Warning)
		} else {
			setColor(theme.Success)
		}
		sb.WriteRune(sparkLevels[level])
	}
	if lastColor != "" {
		sb.WriteString("[-]")
	}
	return sb.String()
}

// sparkThresholds are the RTT increases over the median at which the bar
// grows one level (index+1). An absolute scale keeps small changes on fast
// links (10ms -> 30ms) calm, while treating +20ms the same on any baseline.
var sparkThresholds = []time.Duration{
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond, // sparkWarnLevel
	200 * time.Millisecond,
	500 * time.Millisecond,
}

// sparkWarnLevel is the first level drawn in the warning color (+100ms)
const sparkWarnLevel = 5

// sparkLevel maps the RTT increase over the median to a bar level
func sparkLevel(rtt, median time.Duration) int {
	excess := rtt - median
	level := 0
	for i, th := range sparkThresholds {
		if excess >= th {
			level = i + 1
		}
	}
	return level
}
