// Package statstest provides helpers for tests that need a populated
// stats.MetricsManager built through its public event API.
package statstest

import (
	"testing"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

// NewQuietManager returns a MetricsManager without a failure hook, so tests
// never ring the terminal bell.
func NewQuietManager() stats.MetricsManager {
	return stats.NewMetricsManager()
}

// Feed sends events to mm and blocks until all of them have been processed.
func Feed(t testing.TB, mm stats.MetricsManager, events ...*prober.Event) {
	t.Helper()
	ch := make(chan *prober.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	<-mm.Subscribe(ch)
}

// FromEvents returns a quiet MetricsManager that has processed events.
func FromEvents(t testing.TB, events ...*prober.Event) stats.MetricsManager {
	t.Helper()
	mm := NewQuietManager()
	Feed(t, mm, events...)
	return mm
}

// RegisterProbe registers name as a target of a prober of the given kind
// (icmpv4, http, ...), as a prober's REGISTER event does.
func RegisterProbe(t testing.TB, mm stats.MetricsManager, name string, probeType prober.ProbeType) {
	t.Helper()
	Feed(t, mm, &prober.Event{Key: name, DisplayName: name, Result: prober.REGISTER, ProbeType: probeType})
}

// Register registers each name as a target whose key equals its display name.
func Register(t testing.TB, mm stats.MetricsManager, names ...string) {
	t.Helper()
	events := make([]*prober.Event, 0, len(names))
	for _, n := range names {
		events = append(events, &prober.Event{Key: n, DisplayName: n, Result: prober.REGISTER})
	}
	Feed(t, mm, events...)
}
