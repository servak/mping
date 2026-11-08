package command

import (
	"errors"
	"strings"
	"syscall"
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

func runBatch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewPingBatchCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestBatchRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"negative interval", []string{"-i", "-1", "tcp://127.0.0.1:1"}},
		{"negative timeout", []string{"-t", "-1", "tcp://127.0.0.1:1"}},
		{"missing target file", []string{"-f", "/nonexistent/hosts.txt"}},
		{"directory as target file", []string{"-f", t.TempDir()}},
		{"missing explicit config", []string{"-c", "/nonexistent/mping.yml", "tcp://127.0.0.1:1"}},
		{"no targets", nil},
		{"huge CIDR", []string{"0.0.0.0/0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runBatch(t, tt.args...); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestBatchStopsOnSignal(t *testing.T) {
	done := make(chan error, 1)
	var out string
	go func() {
		var err error
		out, err = runBatch(t, "--count", "100", "-i", "1000", "-t", "200", "-o", "json", "tcp://127.0.0.1:1")
		done <- err
	}()
	time.Sleep(700 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var exitErr *ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "127.0.0.1") {
			t.Errorf("partial report missing: %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not stop after SIGINT")
	}
}
