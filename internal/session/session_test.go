package session

import (
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/config"
)

func TestNewRejectsBadTarget(t *testing.T) {
	_, err := New(Params{
		Config:   config.DefaultConfig(),
		Targets:  []string{"bogusprober:foo"},
		Interval: time.Second,
		Timeout:  time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to add targets") {
		t.Errorf("expected add targets error, got %v", err)
	}
}
