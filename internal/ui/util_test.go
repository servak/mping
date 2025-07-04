package ui

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
			duration: 25 * time.Millisecond,
			expected: " 25ms",
		},
		{
			name:     "sub-millisecond",
			duration: 750 * time.Microsecond,
			expected: "750µs",
		},
		{
			name:     "one second",
			duration: 1 * time.Second,
			expected: "  1s",
		},
		{
			name:     "multiple seconds",
			duration: 2500 * time.Millisecond,
			expected: "  2s", // Seconds()は切り捨て
		},
		{
			name:     "very small duration",
			duration: 100 * time.Nanosecond,
			expected: "  0µs",
		},
		{
			name:     "exactly 1 millisecond",
			duration: 1 * time.Millisecond,
			expected: "  1ms",
		},
		{
			name:     "1.5 seconds",
			duration: 1500 * time.Millisecond,
			expected: "  2s", // Seconds()で四捨五入される
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DurationFormater(tt.duration)
			if result != tt.expected {
				t.Errorf("DurationFormater(%v) = %s, expected %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestTimeFormater(t *testing.T) {
	// テスト用の固定時刻
	fixedTime := time.Date(2023, 12, 25, 15, 30, 45, 0, time.UTC)
	zeroTime := time.Time{}

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "zero time",
			time:     zeroTime,
			expected: "-",
		},
		{
			name:     "normal time",
			time:     fixedTime,
			expected: "15:30:45",
		},
		{
			name:     "midnight",
			time:     time.Date(2023, 12, 25, 0, 0, 0, 0, time.UTC),
			expected: "00:00:00",
		},
		{
			name:     "morning time",
			time:     time.Date(2023, 12, 25, 9, 5, 30, 0, time.UTC),
			expected: "09:05:30",
		},
		{
			name:     "evening time",
			time:     time.Date(2023, 12, 25, 23, 59, 59, 0, time.UTC),
			expected: "23:59:59",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TimeFormater(tt.time)
			if result != tt.expected {
				t.Errorf("TimeFormater(%v) = %s, expected %s", tt.time, result, tt.expected)
			}
		})
	}
}

func TestFormattersConsistency(t *testing.T) {
	// フォーマッターの一貫性をテスト

	// DurationFormaterのゼロ値ハンドリング
	if DurationFormater(0) != "-" {
		t.Error("DurationFormater should return '-' for zero duration")
	}

	// TimeFormaterのゼロ値ハンドリング
	if TimeFormater(time.Time{}) != "-" {
		t.Error("TimeFormater should return '-' for zero time")
	}

	// 小さい値の処理
	smallDuration := 1 * time.Nanosecond
	result := DurationFormater(smallDuration)
	if result != "  0µs" {
		t.Errorf("DurationFormater should handle small durations consistently, got %s", result)
	}
}

func TestFormattersEdgeCases(t *testing.T) {
	// エッジケースのテスト

	t.Run("negative duration", func(t *testing.T) {
		// 負の期間（理論上発生しないはずだが、念のため）
		result := DurationFormater(-100 * time.Millisecond)
		// 負の値の処理は実装依存だが、パニックしないことを確認
		if result == "" {
			t.Error("DurationFormater should not return empty string for negative duration")
		}
	})

	t.Run("very large duration", func(t *testing.T) {
		// 非常に大きな期間
		largeDuration := 1000000 * time.Second
		result := DurationFormater(largeDuration)
		if result == "" {
			t.Error("DurationFormater should handle large durations")
		}
	})

	t.Run("time with different timezone", func(t *testing.T) {
		// 異なるタイムゾーンでのテスト
		jst, _ := time.LoadLocation("Asia/Tokyo")
		timeInJST := time.Date(2023, 12, 25, 15, 30, 45, 0, jst)
		result := TimeFormater(timeInJST)
		
		// 時刻フォーマットはローカル時刻を使用するため、JST時刻がそのまま表示される
		expected := "15:30:45"
		if result != expected {
			t.Errorf("TimeFormater with JST time: expected %s, got %s", expected, result)
		}
	})
}