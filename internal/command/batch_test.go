package command

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/stats"
)

func TestCheckMaxLoss(t *testing.T) {
	now := time.Now()
	metrics := []stats.Metrics{
		stats.NewMetricsForTest("ok", 1, 10, 10, 0, 0, 0, 0, 0, 0, 0, now, time.Time{}, ""),
		stats.NewMetricsForTest("lossy", 1, 10, 7, 3, 30, 0, 0, 0, 0, 0, now, now, "timeout"),
		stats.NewMetricsForTest("down", 1, 10, 0, 10, 100, 0, 0, 0, 0, 0, time.Time{}, now, "timeout"),
	}

	tests := []struct {
		name      string
		maxLoss   float64
		wantErr   bool
		wantHosts []string
	}{
		{"disabled", -1, false, nil},
		{"zero tolerance", 0, true, []string{"lossy", "down"}},
		{"boundary is inclusive", 30, true, []string{"down"}},
		{"all within limit", 100, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMaxLoss(metrics, tt.maxLoss)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected *ExitError, got %v", err)
			}
			if exitErr.Code != ExitCodeThresholdExceeded {
				t.Errorf("exit code = %d, want %d", exitErr.Code, ExitCodeThresholdExceeded)
			}
			for _, h := range tt.wantHosts {
				if !strings.Contains(err.Error(), h+"(") {
					t.Errorf("error %q should mention %s", err, h)
				}
			}
			if strings.Contains(err.Error(), "ok(") {
				t.Errorf("error %q should not mention healthy host", err)
			}
		})
	}
}

func TestBatchCmdRejectsInvalidFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad output", []string{"-o", "xml", "127.0.0.1"}, "unsupported output format"},
		{"bad max-loss", []string{"--max-loss", "150", "127.0.0.1"}, "max-loss must be between"},
		{"bad count", []string{"--count", "0", "127.0.0.1"}, "count must be greater than zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewPingBatchCmd()
			cmd.SetArgs(tt.args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Execute() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
