package shared

import (
	"testing"
	"time"

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
