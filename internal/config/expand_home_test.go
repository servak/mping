package config

import (
	"os/user"
	"testing"
)

func TestExpandHome(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	tests := []struct {
		in, want string
	}{
		{"~/.mping.yml", usr.HomeDir + "/.mping.yml"},
		{"~", usr.HomeDir},
		{"/etc/mping.yml", "/etc/mping.yml"},
		{"relative/~/x", "relative/~/x"},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := ExpandHome(tt.in)
		if err != nil {
			t.Errorf("ExpandHome(%q) error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
