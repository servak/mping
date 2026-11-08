package command

import (
	"github.com/servak/mping/internal/config"
	"github.com/spf13/pflag"
	"os"
	"path/filepath"
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
		{"empty target file", []string{"-f", emptyFile(t)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewPingCmd()
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"-c", emptyConfig(t)}, tt.args...))
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func emptyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hosts.txt")
	if err := os.WriteFile(p, []byte("# nothing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPingCmdNoArgsShowsHelp(t *testing.T) {
	cmd := NewPingCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-c", emptyConfig(t)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("no args should show help without error, got %v", err)
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
