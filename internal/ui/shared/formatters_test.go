package shared

import (
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

func TestDurationFormater(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "-",
		},
		{
			name:     "microseconds",
			duration: 500 * time.Microsecond,
			expected: "500µs",
		},
		{
			name:     "milliseconds",
			duration: 50 * time.Millisecond,
			expected: " 50ms",
		},
		{
			name:     "seconds",
			duration: 2 * time.Second,
			expected: "  2s",
		},
		{
			name:     "edge case - exactly 1000µs",
			duration: 1000 * time.Microsecond,
			expected: "  1ms",
		},
		{
			name:     "edge case - exactly 1000ms",
			duration: 1000 * time.Millisecond,
			expected: "  1s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DurationFormater(tt.duration)
			if result != tt.expected {
				t.Errorf("DurationFormater(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestTimeFormater(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "zero time",
			time:     time.Time{},
			expected: "-",
		},
		{
			name:     "valid time",
			time:     time.Date(2024, 1, 1, 15, 30, 45, 0, time.UTC),
			expected: "15:30:45",
		},
		{
			name:     "midnight",
			time:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "00:00:00",
		},
		{
			name:     "noon",
			time:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			expected: "12:00:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TimeFormater(tt.time)
			if result != tt.expected {
				t.Errorf("TimeFormater(%v) = %s, want %s", tt.time, result, tt.expected)
			}
		})
	}
}

func TestFormatHostDetail(t *testing.T) {
	testTime := time.Date(2024, 1, 1, 15, 30, 45, 0, time.UTC)

	metric := stats.NewMetricsForTest(
		"example.com",
		1,
		100,
		95,
		5,
		5.0,
		25*time.Millisecond,
		30*time.Millisecond,
		20*time.Millisecond,
		40*time.Millisecond,
		25*time.Millisecond,
		testTime,
		testTime.Add(time.Second),
		"timeout",
	)

	theme := &Theme{
		Primary:   "#ffffff",
		Secondary: "#cccccc",
		Success:   "#00ff00",
		Warning:   "#ffff00",
		Error:     "#ff0000",
		Accent:    "#00afd7",
		Separator: "#666666",
		Timestamp: "#999999",
	}
	result := FormatHostDetail(metric, theme)

	expectedContents := []string{
		"[#00afd7]Total Probes:[#ffffff] 100",
		"[#00ff00]Successful:[#ffffff] 95",
		"[#ff0000]Failed:[#ffffff] 5",
		"[#00afd7]Loss Rate:[#ffffff]",
		"[#00ff00]5.0%[#ffffff]",
		"[#00afd7]Last RTT:[#ffffff]  25ms",
		"[#00afd7]Average RTT:[#ffffff]  30ms",
		"[#00afd7]Minimum RTT:[#ffffff]  20ms",
		"[#00afd7]Maximum RTT:[#ffffff]  40ms",
		"[#00afd7]Last Success:[#ffffff] 15:30:45",
		"[#00afd7]Last Failure:[#ffffff] 15:30:46",
		"[#00afd7]Last Error:[#ffffff] timeout",
	}

	for _, expected := range expectedContents {
		if !contains(result, expected) {
			t.Errorf("FormatHostDetail result missing expected content: %s\nActual result:\n%s", expected, result)
		}
	}
}

func TestFormatHostDetailWithZeroValues(t *testing.T) {
	metric := stats.NewMetricsForTest(
		"test.com",
		1,
		0,
		0,
		0,
		0.0,
		0,
		0,
		0,
		0,
		0,
		time.Time{},
		time.Time{},
		"",
	)

	theme := &Theme{
		Primary:   "#ffffff",
		Secondary: "#cccccc",
		Success:   "#00ff00",
		Warning:   "#ffff00",
		Error:     "#ff0000",
		Accent:    "#00afd7",
		Separator: "#666666",
		Timestamp: "#999999",
	}
	result := FormatHostDetail(metric, theme)

	expectedContents := []string{
		"[#00afd7]Total Probes:[#ffffff] 0",
		"[#ff0000]Successful:[#ffffff] 0",
		"[#ffffff]Failed:[#ffffff] 0",
		"[#00afd7]Loss Rate:[#ffffff]",
		"[#00ff00]0.0%[#ffffff]",
		"[#00afd7]Last RTT:[#ffffff] -",
		"[#00afd7]Average RTT:[#ffffff] -",
		"[#00afd7]Minimum RTT:[#ffffff] -",
		"[#00afd7]Maximum RTT:[#ffffff] -",
		"[#00afd7]Last Success:[#ffffff] -",
		"[#00afd7]Last Failure:[#ffffff] -",
		"[#00afd7]Last Error:[#ffffff] ",
	}

	for _, expected := range expectedContents {
		if !contains(result, expected) {
			t.Errorf("FormatHostDetail result missing expected content: %s\nActual result:\n%s", expected, result)
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFormatSparkline(t *testing.T) {
	theme := &Theme{Success: "green", Warning: "yellow", Error: "red"}
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	// GetRecentHistory order: newest first
	history := []stats.HistoryEntry{
		{Success: true, RTT: ms(40)}, // newest: +30ms over the 10ms median
		{Success: false},
		{Success: true, RTT: ms(10)},
		{Success: true, RTT: ms(11)}, // normal jitter stays at the lowest bar
		{Success: true, RTT: ms(10)}, // oldest
	}

	got := FormatSparkline(history, 8, theme)
	want := "   [green]▁▁▁[red]×[green]▄[-]"
	if got != want {
		t.Errorf("FormatSparkline() = %q, want %q", got, want)
	}

	// Truncated to width, keeping the newest entries
	if got := FormatSparkline(history, 2, theme); got != "[red]×[green]▄[-]" {
		t.Errorf("truncated sparkline = %q", got)
	}
	if got := FormatSparkline(nil, 3, theme); got != "   " {
		t.Errorf("empty history should be blank padding, got %q", got)
	}
}

func TestSparkLevel(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	tests := []struct {
		name        string
		rtt, median time.Duration
		want        int
	}{
		{"faster than usual", ms(8), ms(10), 0},
		{"normal jitter", ms(14), ms(10), 0},
		{"+5ms boundary", ms(15), ms(10), 1},
		{"10ms -> 30ms is only +20ms", ms(30), ms(10), 3},
		{"same +20ms on a 150ms baseline", ms(170), ms(150), 3},
		{"+99ms stays below warning", ms(249), ms(150), 4},
		{"+100ms boundary is warning", ms(250), ms(150), sparkWarnLevel},
		{"150ms -> 400ms", ms(400), ms(150), 6},
		{"+500ms and beyond is the top bar", ms(2000), ms(10), 7},
	}
	for _, tt := range tests {
		if got := sparkLevel(tt.rtt, tt.median); got != tt.want {
			t.Errorf("%s: sparkLevel(%v, %v) = %d, want %d", tt.name, tt.rtt, tt.median, got, tt.want)
		}
	}
}

func TestFormatSparklineWarnsOnLargeIncrease(t *testing.T) {
	theme := &Theme{Success: "green", Warning: "yellow", Error: "red"}
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	history := []stats.HistoryEntry{ // newest first
		{Success: true, RTT: ms(400)},
		{Success: true, RTT: ms(150)},
		{Success: true, RTT: ms(150)},
	}
	if got := FormatSparkline(history, 3, theme); got != "[green]▁▁[yellow]▇[-]" {
		t.Errorf("FormatSparkline() = %q", got)
	}
}

func TestFormatOutages(t *testing.T) {
	theme := PredefinedThemes["dark"]
	none := stats.NewMetricsForTest("h", 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, time.Time{}, time.Time{}, "")
	if got := FormatOutages(none, &theme); !strings.Contains(got, "none") {
		t.Errorf("expected 'none', got %q", got)
	}
}

func TestFormatLastFailCell(t *testing.T) {
	theme := &Theme{Warning: "yellow", Error: "red"}
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// build feeds one probe per second: 'S' success, 'F' failure
	build := func(pattern string) stats.Metrics {
		mm := stats.NewMetricsManager()
		mm.ToggleBeep()
		ch := make(chan *prober.Event, len(pattern)+1)
		ch <- &prober.Event{Key: "h", DisplayName: "h", Result: prober.REGISTER}
		for i, c := range pattern {
			ev := &prober.Event{Key: "h", SentTime: base.Add(time.Duration(i) * time.Second)}
			if c == 'S' {
				ev.Result = prober.SUCCESS
			} else {
				ev.Result, ev.Message = prober.TIMEOUT, "timeout"
			}
			ch <- ev
		}
		close(ch)
		<-mm.Subscribe(ch)
		return mm.SortBy(stats.Host, true)[0]
	}

	now := base.Add(20 * time.Second)
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{"no failure", "SSS", "-"},
		{"recovered outage", "SFFFS", "[yellow]10:00:03 (3.00s)[-]"},
		{"ongoing outage", "SSFFF", "[red]10:00:04 (DOWN 18.0s)[-]"},
		{"loss shorter than an outage", "SSFS", "10:00:02"},
		{"isolated failure after an outage", "SFFFSSFS", "10:00:06"},
	}
	for _, tt := range tests {
		if got := FormatLastFailCell(build(tt.pattern), now, theme); got != tt.want {
			t.Errorf("%s: FormatLastFailCell(%s) = %q, want %q", tt.name, tt.pattern, got, tt.want)
		}
	}
}

func TestFormatShortDuration(t *testing.T) {
	tests := map[time.Duration]string{
		1600 * time.Millisecond:       "1.60s",
		36700 * time.Millisecond:      "36.7s",
		2*time.Minute + 3*time.Second: "2m03s",
		3*time.Hour + 5*time.Minute:   "3h05m",
	}
	for d, want := range tests {
		if got := FormatShortDuration(d); got != want {
			t.Errorf("FormatShortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
