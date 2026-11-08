package command

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func emptyConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mping.yml")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runBatchWith(t *testing.T, notify signalSource, args ...string) (string, error) {
	t.Helper()
	cmd := newPingBatchCmd(notify)
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"-c", emptyConfig(t)}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func runBatch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runBatchWith(t, osSignals, args...)
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

func TestBatchInterruptedExitsNonZero(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGTERM
	var stopped atomic.Bool
	notify := func() (<-chan os.Signal, func()) {
		return sigs, func() { stopped.Store(true) }
	}
	done := make(chan error, 1)
	var out string
	go func() {
		var err error
		out, err = runBatchWith(t, notify, "--count", "100", "-i", "1000", "-t", "200", "-o", "json", "tcp://127.0.0.1:1")
		done <- err
	}()
	select {
	case err := <-done:
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected *ExitError, got %v", err)
		}
		if want := 128 + int(syscall.SIGTERM); exitErr.Code != want {
			t.Errorf("exit code = %d, want %d", exitErr.Code, want)
		}
		if !strings.Contains(err.Error(), "partial") {
			t.Errorf("error %q should say the report is partial", err)
		}
		// The signal may arrive before the first probe result, so the report
		// may legitimately be empty; it must still be well-formed.
		var report []map[string]any
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Errorf("partial report is not valid JSON: %v: %q", err, out)
		}
		if !stopped.Load() {
			t.Error("signal handler was not released on first signal")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("batch did not stop after signal")
	}
}
