//go:build integration

package trace

// Integration tests against the real network. Run with:
//
//	go test -tags integration -run Integration -v ./internal/trace/
//
// Environment:
//
//	MPING_IT_TARGETS   comma separated targets (default 8.8.8.8,1.1.1.1,192.168.1.1)
//	MPING_IT_ROUNDS    rounds per target (default 20)
//	MPING_IT_INTERVAL  interval between rounds (default 1s, same as the TUI)

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type probeKey struct{ round, ttl int }

// traceLog collects per-probe events of one tracer
type traceLog struct {
	mu      sync.Mutex
	results map[probeKey][]probeEvent
	late    int
}

func (l *traceLog) record(e probeEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.outcome == outcomeLate {
		l.late++
		return
	}
	k := probeKey{e.round, e.ttl}
	l.results[k] = append(l.results[k], e)
}

func TestIntegrationTrace(t *testing.T) {
	targets := strings.Split(envOr("MPING_IT_TARGETS", "8.8.8.8,1.1.1.1,192.168.1.1"), ",")
	rounds, _ := strconv.Atoi(envOr("MPING_IT_ROUNDS", "20"))
	interval, _ := time.ParseDuration(envOr("MPING_IT_INTERVAL", "1s"))

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			tr, err := New(target, Config{Interval: interval, Timeout: time.Second, NoDNS: true})
			if err != nil {
				t.Skipf("cannot trace: %v", err)
			}
			log := &traceLog{results: map[probeKey][]probeEvent{}}
			tr.onEvent = log.record

			if err := tr.Run(context.Background(), rounds); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			res := tr.Snapshot()
			t.Logf("privileged=%v reached=%v rounds=%d hops=%d late=%d",
				res.Privileged, res.Reached, res.Rounds, len(res.Hops), log.late)
			t.Log("\n" + renderGrid(res, log))

			checkInvariants(t, res, log)
		})
	}
}

// renderGrid shows every probe: '.' reply, 'x' lost, '?' never resolved,
// '!' resolved more than once. Rows are hops, columns are rounds.
func renderGrid(res Result, log *traceLog) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "hop %-16s %s\n", "addr", "rounds 1..")
	for _, h := range res.Hops {
		fmt.Fprintf(&sb, "%3d %-16s ", h.TTL, h.Addr)
		for r := 1; r <= res.Rounds; r++ {
			evs := log.results[probeKey{r, h.TTL}]
			switch {
			case len(evs) == 0:
				sb.WriteByte('?')
			case len(evs) > 1:
				sb.WriteByte('!')
			case evs[0].outcome == outcomeReply:
				sb.WriteByte('.')
			default:
				sb.WriteByte('x')
			}
		}
		fmt.Fprintf(&sb, "  loss=%5.1f%% sent=%d recv=%d\n", h.Loss(), h.Sent, h.Recv)
	}
	return sb.String()
}

func checkInvariants(t *testing.T, res Result, log *traceLog) {
	t.Helper()
	for _, h := range res.Hops {
		// Every probe sent to a displayed hop must be resolved exactly once.
		// A hop is probed every round from the first round that reached it
		// (the probe limit grows as farther hops reply).
		first := 1
		for first <= res.Rounds && len(log.results[probeKey{first, h.TTL}]) == 0 {
			first++
		}
		if want := res.Rounds - first + 1; h.Sent != want {
			t.Errorf("hop %d: sent=%d, want %d (one settled probe per round from round %d)", h.TTL, h.Sent, want, first)
		}
		for r := first; r <= res.Rounds; r++ {
			if n := len(log.results[probeKey{r, h.TTL}]); n != 1 {
				t.Errorf("hop %d round %d: resolved %d times, want 1", h.TTL, r, n)
			}
		}
		// A hop that answered and then never again is suspicious: either the
		// path changed or replies are being mismatched/expired.
		lastReply, firstReply := 0, 0
		for r := 1; r <= res.Rounds; r++ {
			if evs := log.results[probeKey{r, h.TTL}]; len(evs) == 1 && evs[0].outcome == outcomeReply {
				if firstReply == 0 {
					firstReply = r
				}
				lastReply = r
			}
		}
		if firstReply > 0 && res.Rounds-lastReply >= 5 {
			// Routers rate-limit Time Exceeded (verified against the system
			// traceroute), so this is only a problem if the loss also shows
			// at the destination.
			msg := fmt.Sprintf("hop %d answered in rounds %d..%d then stayed silent for %d rounds",
				h.TTL, firstReply, lastReply, res.Rounds-lastReply)
			if res.HasRateLimitedHops() {
				t.Log(msg + " (router ICMP rate limiting: destination is clean)")
			} else {
				t.Error(msg)
			}
		}
	}
	if log.late > 0 {
		t.Errorf("%d replies arrived after their probe was counted as lost", log.late)
	}
	if !res.Reached {
		t.Errorf("destination never replied")
	}
}
