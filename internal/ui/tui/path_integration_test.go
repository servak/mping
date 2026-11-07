//go:build integration

package tui

// Drives the real layout + path tracer while the host list is re-sorted,
// reproducing the "path view keeps resetting" report. Run with:
//
//	go test -tags integration -run Integration -v ./internal/ui/tui/
//
// MPING_IT_DURATION controls how long each scenario samples (default 12s).
// MPING_IT_HOSTS sets the hosts (default 8.8.8.8,1.1.1.1,192.168.1.1). The
// first one times out once and jumps to the top of the list; the path view
// must keep following the host selected before that. CI uses loopback hosts
// (127.0.0.3,127.0.0.1,127.0.0.2) because hosted runners block outbound ICMP.
// MPING_IT_STRICT=1 fails instead of skipping when tracing is unavailable.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

func TestIntegrationPathViewFollowsSelection(t *testing.T) {
	// ICMP targets are traced with ICMP, every other prober kind with UDP
	for _, tc := range []struct {
		probeType prober.ProbeType
		want      trace.Proto
	}{
		{"icmpv4", trace.ProtoICMP},
		{"tcp", trace.ProtoUDP},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			runPathViewFollowsSelection(t, tc.probeType, tc.want)
		})
	}
}

func runPathViewFollowsSelection(t *testing.T, probeType prober.ProbeType, wantProto trace.Proto) {
	duration := 12 * time.Second
	if v, err := time.ParseDuration(os.Getenv("MPING_IT_DURATION")); err == nil {
		duration = v
	}

	mm := statstest.NewQuietManager()
	events := make(chan *prober.Event, 1000)
	done := mm.Subscribe(events)
	defer func() { close(events); <-done }()

	hosts := []string{"8.8.8.8", "1.1.1.1", "192.168.1.1"}
	if v := os.Getenv("MPING_IT_HOSTS"); v != "" {
		hosts = strings.Split(v, ",")
	}
	flapping := hosts[0]
	for _, h := range hosts {
		events <- &prober.Event{Key: h, DisplayName: h, Result: prober.REGISTER, ProbeType: probeType}
	}
	// Events are processed asynchronously; select only once every host is listed
	for deadline := time.Now().Add(5 * time.Second); len(mm.SortBy(stats.Host, true)) < len(hosts); {
		if time.Now().After(deadline) {
			t.Fatal("hosts were not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	uiState := state.NewUIState()
	layout := NewLayoutManager(uiState, mm, nil, shared.DefaultConfig(), 500*time.Millisecond, time.Second)
	defer layout.StopPathTrace()
	layout.UpdateAll()
	layout.TogglePathView()
	layout.UpdateAll() // starts following the selected host
	// The host selected before anything fails; it must stay selected.
	want, _, _ := layout.pathTracer.State()
	if want == "" || want == flapping {
		t.Fatalf("initial path target = %q, want one of the healthy hosts %v", want, hosts[1:])
	}

	start := time.Now()
	failed := false
	lastRounds := 0
	for time.Since(start) < duration {
		now := time.Now()
		for _, h := range hosts {
			events <- &prober.Event{Key: h, Result: prober.SUCCESS, SentTime: now, Rtt: time.Millisecond}
		}
		// Like the report: one host times out once and jumps to the top
		if !failed && time.Since(start) > duration/3 {
			events <- &prober.Event{Key: flapping, Result: prober.TIMEOUT, SentTime: now, Message: "timeout"}
			failed = true
		}
		time.Sleep(250 * time.Millisecond) // TUI refresh period

		layout.UpdateAll()
		target, res, err := layout.pathTracer.State()
		if err != nil {
			if os.Getenv("MPING_IT_STRICT") == "1" {
				t.Fatalf("path tracing unavailable: %v", err)
			}
			t.Skipf("path tracing unavailable: %v", err)
		}
		if target != want {
			t.Fatalf("path target switched to %q at %v, want it to stay on %s",
				target, time.Since(start).Round(time.Millisecond), want)
		}
		if res == nil {
			continue
		}
		if res.Proto != wantProto {
			t.Fatalf("traced with %q, want %q", res.Proto, wantProto)
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
	t.Logf("path to %s kept for %v: %d rounds, %d hops, reached=%v", want, duration, res.Rounds, len(res.Hops), res.Reached)
}
