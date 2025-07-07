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
	
	metric := stats.Metrics{
		Name:           "example.com",
		Total:          100,
		Successful:     95,
		Failed:         5,
		Loss:           5.0,
		LastRTT:        25 * time.Millisecond,
		AverageRTT:     30 * time.Millisecond,
		MinimumRTT:     20 * time.Millisecond,
		MaximumRTT:     40 * time.Millisecond,
		LastSuccTime:   testTime,
		LastFailTime:   testTime.Add(time.Second),
		LastFailDetail: "timeout",
	}

	result := FormatHostDetail(metric)

	expectedContents := []string{
		"Host Details: example.com",
		"Total Probes: 100",
		"Successful: 95",
		"Failed: 5",
		"Loss Rate: 5.0%",
		"Last RTT:  25ms",
		"Average RTT:  30ms",
		"Minimum RTT:  20ms",
		"Maximum RTT:  40ms",
		"Last Success: 15:30:45",
		"Last Failure: 15:30:46",
		"Last Error: timeout",
	}

	for _, expected := range expectedContents {
		if !contains(result, expected) {
			t.Errorf("FormatHostDetail result missing expected content: %s\nActual result:\n%s", expected, result)
		}
	}
}

func TestFormatHostDetailWithZeroValues(t *testing.T) {
	metric := stats.Metrics{
		Name:           "test.com",
		Total:          0,
		Successful:     0,
		Failed:         0,
		Loss:           0.0,
		LastRTT:        0,
		AverageRTT:     0,
		MinimumRTT:     0,
		MaximumRTT:     0,
		LastSuccTime:   time.Time{},
		LastFailTime:   time.Time{},
		LastFailDetail: "",
	}

	result := FormatHostDetail(metric)

	expectedContents := []string{
		"Host Details: test.com",
		"Total Probes: 0",
		"Successful: 0",
		"Failed: 0",
		"Loss Rate: 0.0%",
		"Last RTT: -",
		"Average RTT: -",
		"Minimum RTT: -",
		"Maximum RTT: -",
		"Last Success: -",
		"Last Failure: -",
		"Last Error: ",
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