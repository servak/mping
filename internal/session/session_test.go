package session

import (
	"context"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/stats"
)

// params returns the parameters of a fast session on the given targets.
func params(cfg *config.Config, targets ...string) Params {
	return Params{Config: cfg, Targets: targets, Interval: 20 * time.Millisecond, Timeout: time.Second}
}

// only returns the metrics of the single target of s.
func only(t *testing.T, s *Session) stats.Metrics {
	t.Helper()
	m := s.Metrics().SortBy(stats.Host, true)
	if len(m) != 1 {
		t.Fatalf("session has %d targets, want 1", len(m))
	}
	return m[0]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitTotal waits until every session has sent at least n probes to its
// single target.
func waitTotal(t *testing.T, n int, sessions ...*Session) {
	t.Helper()
	waitFor(t, "probes", func() bool {
		for _, s := range sessions {
			m := s.Metrics().SortBy(stats.Host, true)
			if len(m) != 1 || m[0].GetTotal() < n {
				return false
			}
		}
		return true
	})
}

// tcpServer accepts connections on loopback and returns its address.
func tcpServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	})
	return ln.Addr().String()
}

func TestNewRejectsBadTarget(t *testing.T) {
	_, err := New(params(config.DefaultConfig(), "bogusprober:foo"))
	if err == nil || !strings.Contains(err.Error(), "failed to add targets") {
		t.Errorf("expected add targets error, got %v", err)
	}
}

// Sessions created at the same time from one config, with different source
// interfaces, probe the same target each from its own source, and leave the
// config alone. The one whose source address is not of this host fails.
func TestSessionsDoNotShareConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	target := "tcp://" + tcpServer(t)
	sources := []string{"127.0.0.1", "192.0.2.77", "", "192.0.2.78"}

	sessions := make([]*Session, len(sources))
	errs := make([]error, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Go(func() {
			p := params(cfg, target)
			p.SourceInterface = source
			sessions[i], errs[i] = New(p)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sessions[i].Stop)
	}
	if !reflect.DeepEqual(cfg, config.DefaultConfig()) {
		t.Errorf("config was modified:\n%s", config.Marshal(cfg))
	}

	for _, s := range sessions {
		s.Start(context.Background())
	}
	waitTotal(t, 5, sessions...)
	for i, s := range sessions {
		s.Stop()
		m := only(t, s)
		want, got := m.GetTotal(), m.GetSuccessful()
		if strings.HasPrefix(sources[i], "192.0.2.") {
			got = m.GetFailed()
		}
		if got != want {
			t.Errorf("source %q: %d sent, %d successful, %d failed (%s)", sources[i],
				m.GetTotal(), m.GetSuccessful(), m.GetFailed(), m.GetLastFailDetail())
		}
	}
}
