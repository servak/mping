package command

import (
	"github.com/servak/mping/internal/config"
	"github.com/spf13/pflag"
	"strings"
	"testing"
)

func TestPingCmdRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"negative interval", []string{"-i", "-1", "127.0.0.1"}},
		{"negative timeout", []string{"-t", "-1", "127.0.0.1"}},
		{"missing target file", []string{"-f", "/nonexistent/hosts.txt"}},
		{"missing explicit config", []string{"-c", "/nonexistent/mping.yml", "127.0.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewPingCmd()
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestApplyTitle(t *testing.T) {
	newFlags := func(args ...string) *pflag.FlagSet {
		fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
		fs.StringP("title", "n", "", "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return fs
	}

	cfg := config.DefaultConfig()
	if err := applyTitle(cfg, newFlags()); err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Title != "mping" {
		t.Errorf("default title overwritten: %q", cfg.UI.Title)
	}

	if err := applyTitle(cfg, newFlags("-n", "custom")); err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Title != "custom" {
		t.Errorf("title = %q, want custom", cfg.UI.Title)
	}
}
