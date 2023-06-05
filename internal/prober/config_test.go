package prober

import (
	"testing"
	"time"
)

func TestConvertToDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"5s", 5 * time.Second, false},
		{"1ms", 1 * time.Millisecond, false},
		{"10s", 10 * time.Second, false},
		{"500ms", 500 * time.Millisecond, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := convertToDuration(tt.input)

			if (err != nil) != tt.err {
				t.Errorf("convertToDuration(%q) returned error %v, want error? %v", tt.input, err, tt.err)
			}

			if got != tt.want {
				t.Errorf("convertToDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
