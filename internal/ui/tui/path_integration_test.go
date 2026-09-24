//go:build integration

package tui

// Drives the real layout + path tracer while the host list is re-sorted,
// reproducing the "path view keeps resetting" report. Run with:
//
//	go test -tags integration -run Integration -v ./internal/ui/tui/
//
// MPING_IT_DURATION controls how long each scenario samples (default 12s).

import (
	"os"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

func TestIntegrationPathViewFollowsSelection(t *testing.T) {
	duration := 12 * time.Second
	if v, err := time.ParseDuration(os.Getenv("MPING_IT_DURATION")); err == nil {
		duration = v
	}

	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	events := make(chan *prober.Event, 1000)
	done := mm.Subscribe(events)
	defer func() { close(events); <-done }()

	hosts := []string{"8.8.8.8", "1.1.1.1", "192.168.1.1"}
	for _, h := range hosts {
		events <- &prober.Event{Key: h, DisplayName: h, Result: prober.REGISTER}
	}

	uiState := state.NewUIState()
	layout := NewLayoutManager(uiState, mm, shared.DefaultConfig(), 500*time.Millisecond, time.Second)
	defer layout.StopPathTrace()
	layout.UpdateAll()
	layout.TogglePathView()

	start := time.Now()
	failed := false
	lastRounds := 0
	for time.Since(start) < duration {
		now := time.Now()
		for _, h := range hosts {
			events <- &prober.Event{Key: h, Result: prober.SUCCESS, SentTime: now, Rtt: time.Millisecond}
		}
		// Like the report: 8.8.8.8 times out once and jumps to the top
		if !failed && time.Since(start) > duration/3 {
			events <- &prober.Event{Key: "8.8.8.8", Result: prober.TIMEOUT, SentTime: now, Message: "timeout"}
			failed = true
		}
		time.Sleep(250 * time.Millisecond) // TUI refresh period

		layout.UpdateAll()
		target, res, err := layout.pathTracer.State()
		if err != nil {
			t.Skipf("path tracing unavailable: %v", err)
		}
		if target != "1.1.1.1" {
			t.Fatalf("path target switched to %q at %v, want it to stay on 1.1.1.1",
				target, time.Since(start).Round(time.Millisecond))
		}
		if res == nil {
			continue
		}
		if res.Rounds < lastRounds {
			t.Fatalf("rounds went back from %d to %d: the trace was restarted", lastRounds, res.Rounds)
		}
		lastRounds = res.Rounds
	}

	_, res, _ := layout.pathTracer.State()
	if res == nil || res.Rounds < int(duration/time.Second)-2 {
		t.Fatalf("trace made too little progress: %+v", res)
	}
	t.Logf("path to 1.1.1.1 kept for %v: %d rounds, %d hops, reached=%v", duration, res.Rounds, len(res.Hops), res.Reached)
}
