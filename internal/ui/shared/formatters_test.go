package shared

import (
	"testing"
	"time"
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
