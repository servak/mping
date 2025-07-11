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

	result := FormatHostDetail(metric)

	expectedContents := []string{
		"Total Probes:[white] 100",
		"Successful:[white] 95",
		"Failed:[white] 5",
		"Loss Rate:[white]",
		"5.0%",
		"Last RTT:[white]  25ms",
		"Average RTT:[white]  30ms",
		"Minimum RTT:[white]  20ms",
		"Maximum RTT:[white]  40ms",
		"Last Success:[white] 15:30:45",
		"Last Failure:[white] 15:30:46",
		"Last Error:[white] timeout",
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

	result := FormatHostDetail(metric)

	expectedContents := []string{
		"Total Probes:[white] 0",
		"Successful:[white] 0",
		"Failed:[white] 0",
		"Loss Rate:[white]",
		"0.0%",
		"Last RTT:[white] -",
		"Average RTT:[white] -",
		"Minimum RTT:[white] -",
		"Maximum RTT:[white] -",
		"Last Success:[white] -",
		"Last Failure:[white] -",
		"Last Error:[white] ",
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
