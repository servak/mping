package shared

import (
	"fmt"
	"time"
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
