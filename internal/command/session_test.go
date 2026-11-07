package command

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestValidateTiming(t *testing.T) {
	tests := []struct {
		name         string
		interval     int
		timeout      int
		wantErr      string
		wantInterval time.Duration
		wantTimeout  time.Duration
	}{
		{"valid", 1000, 500, "", time.Second, 500 * time.Millisecond},
		{"both zero", 0, 0, "both interval and timeout can't be zero", 0, 0},
		{"interval zero", 0, 1000, "interval can't be zero", 0, 0},
		{"timeout zero", 1000, 0, "timeout can't be zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, to, err := validateTiming(tt.interval, tt.timeout)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if i != tt.wantInterval || to != tt.wantTimeout {
				t.Errorf("got (%v, %v), want (%v, %v)", i, to, tt.wantInterval, tt.wantTimeout)
			}
		})
	}
}

func TestParseTimingFlags(t *testing.T) {
	newFlags := func() *pflag.FlagSet {
		fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
		fs.Int("interval", 1000, "")
		fs.Int("timeout", 1000, "")
		return fs
	}

	fs := newFlags()
	if err := fs.Parse([]string{"--interval", "250"}); err != nil {
		t.Fatal(err)
	}
	i, to, err := parseTimingFlags(fs)
	if err != nil || i != 250*time.Millisecond || to != time.Second {
		t.Errorf("got (%v, %v, %v)", i, to, err)
	}

	fs = newFlags()
	if err := fs.Parse([]string{"--timeout", "0"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseTimingFlags(fs); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got %v", err)
	}

	// Missing flags surface as errors rather than silent zeros.
	if _, _, err := parseTimingFlags(pflag.NewFlagSet("empty", pflag.ContinueOnError)); err == nil {
		t.Error("expected error for undefined flags")
	}
}

func TestLoadConfigFallsBackToDefaults(t *testing.T) {
	cfg, err := loadConfig("/nonexistent/mping.yml")
	if err != nil || cfg == nil {
		t.Fatalf("expected default config, got (%v, %v)", cfg, err)
	}
}

func TestNewSessionRejectsBadTarget(t *testing.T) {
	cfg, err := loadConfig("/nonexistent/mping.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = newSession(sessionParams{
		Cfg:      cfg,
		Hosts:    []string{"bogusprober:foo"},
		Interval: time.Second,
		Timeout:  time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to add targets") {
		t.Errorf("expected add targets error, got %v", err)
	}
}
