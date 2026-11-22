package command

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/ui/shared"
)

// at is a time of the zone that all are shown in.
func at(hour, minute, second int) time.Time {
	return time.Date(2026, 11, 22, hour, minute, second, 120e6, time.Local)
}

func ptr[T any](v T) *T { return &v }

var exampleWatch = api.Watch{
	ID: "w-0123456789ab", Name: "demo", State: api.StateRunning,
	StartedAt: at(14, 0, 0), ExpiresAt: at(16, 0, 0), IntervalMs: 1000, TimeoutMs: 500,
	TargetCount: 1024, Counts: api.Counts{Down: 1, Degraded: 1, Pending: 2, OK: 1020},
}

func wantText(t *testing.T, got, want string) {
	t.Helper()
	if got = strings.TrimSpace(got); got != strings.TrimSpace(want) {
		t.Errorf("got\n%s\nwant%s", got, want)
	}
}

func TestWatchTable(t *testing.T) {
	stopped := exampleWatch
	stopped.ID, stopped.State, stopped.Reason, stopped.StoppedAt = "w-ba9876543210", api.StateStopped, api.ReasonExpired, ptr(at(13, 0, 0))
	stopping := exampleWatch
	stopping.Name, stopping.State, stopping.Reason = "x", api.StateStopping, api.ReasonRequested
	stopping.ExpiresAt = at(14, 0, 0)
	wantText(t, watchTable([]api.Watch{stopped, exampleWatch, stopping}, at(14, 10, 3)), `
┌──────┬────────────────┬──────────────────────┬─────────┬──────┬──────────┬────────────┬─────────────────────┐
│ NAME │ ID             │ STATE                │ TARGETS │ DOWN │ DEGRADED │ OK/PENDING │ LEFT/STOPPED        │
├──────┼────────────────┼──────────────────────┼─────────┼──────┼──────────┼────────────┼─────────────────────┤
│ demo │ w-ba9876543210 │ stopped (expired)    │    1024 │    1 │        1 │ 1020/2     │ 2026-11-22 13:00:00 │
│ demo │ w-0123456789ab │ running              │    1024 │    1 │        1 │ 1020/2     │ 1h49m               │
│ x    │ w-0123456789ab │ stopping (requested) │    1024 │    1 │        1 │ 1020/2     │ 0.00s               │
└──────┴────────────────┴──────────────────────┴─────────┴──────┴──────────┴────────────┴─────────────────────┘
`)
}

func TestSummaryText(t *testing.T) {
	s := api.Summary{Watch: exampleWatch, LastEventSeq: 5}
	wantText(t, summaryText(s, at(14, 59, 18)), `
demo (w-0123456789ab): running
started:  2026-11-22 14:00:00
ends:     2026-11-22 16:00:00 (in 1h00m)
probes:   every 1s, timeout 500ms
result:   1024 targets: 1 down, 1 degraded, 2 pending, 1020 ok
`)

	s.State, s.Reason, s.Error, s.StoppedAt = api.StateStopped, api.ReasonFailed, "probing ended\x1b[2J", ptr(at(15, 0, 0))
	s.SourceInterface = "eth0"
	s.GapCount, s.LastGap = 2, &api.Gap{Start: at(14, 10, 0), End: at(14, 25, 0), DurationMs: 900000}
	s.DroppedEvents, s.LastEventSeq = 7, 10007
	s.OmittedTargets = 3
	s.Targets = []api.TargetSummary{{
		Status: api.StatusDown, ConsecutiveFailures: 42,
		TargetReport: shared.TargetReport{
			Host: "core\x07(10.0.0.1)", LossPercent: 66.666, OutageCount: 2, TotalDowntimeMs: 15000.4,
			LastFailAt: ptr(at(14, 59, 59)), LastFailReason: strings.Repeat("timeout ", 20),
		},
	}, {Status: api.StatusPending, TargetReport: shared.TargetReport{Host: "new"}}}
	wantText(t, summaryText(s, at(15, 0, 0)), `
demo (w-0123456789ab): stopped (failed)
error:    probing ended[2J
started:  2026-11-22 14:00:00
stopped:  2026-11-22 15:00:00
probes:   every 1s, timeout 500ms, from eth0
result:   1024 targets: 1 down, 1 degraded, 2 pending, 1020 ok
gaps:     2 without probes, the last one 2026-11-22 14:10:00 to 2026-11-22 14:25:00 (15m0s)
events:   10007, of which the first 7 have been dropped
┌─────────┬────────────────┬───────┬───────────────────┬───────────┬─────────────────────┬──────────────────────────────────────────────────────────────────────────────────┐
│ STATUS  │ HOST           │ LOSS  │ CONSECUTIVE FAILS │ OUTAGES   │ LAST FAIL           │ REASON                                                                           │
├─────────┼────────────────┼───────┼───────────────────┼───────────┼─────────────────────┼──────────────────────────────────────────────────────────────────────────────────┤
│ down    │ core(10.0.0.1) │ 66.7% │                42 │ 2/15.000s │ 2026-11-22 14:59:59 │ timeout timeout timeout timeout timeout timeout timeout timeout timeout timeout… │
│ pending │ new            │ 0.0%  │                 0 │ -         │ -                   │                                                                                  │
└─────────┴────────────────┴───────┴───────────────────┴───────────┴─────────────────────┴──────────────────────────────────────────────────────────────────────────────────┘
and 3 more that are not ok, see --all
`)
}

func TestEventLine(t *testing.T) {
	outage := &api.Outage{OutageReport: shared.OutageReport{DurationMs: 36750, LostProbes: 368, LastError: "time\x1bout"}}
	unknown := *outage
	unknown.EndUnknown = true
	for _, tc := range []struct {
		e    api.Event
		want string
	}{
		{api.Event{Kind: "outage_start", Target: "h", Outage: outage}, "outage_start h: 368 lost, timeout"},
		{api.Event{Kind: "outage_end", Target: "h", Outage: outage}, "outage_end h: down for 36.750s, 368 lost, timeout"},
		{api.Event{Kind: "outage_end", Target: "h", Outage: &unknown}, "outage_end h: still down for 36.750s, 368 lost, timeout"},
		{api.Event{Kind: "ttl_change", Target: "h", TTL: &api.TTLChange{Old: 54, New: 52, HopsDelta: ptr(2)}}, "ttl_change h: 54 -> 52 (+2 hops)"},
		{api.Event{Kind: "ttl_change", Target: "h", TTL: &api.TTLChange{Old: 54, New: 55, HopsDelta: ptr(-1)}}, "ttl_change h: 54 -> 55 (-1 hops)"},
		{api.Event{Kind: "ttl_change", Target: "h", TTL: &api.TTLChange{Old: 54, New: 245}}, "ttl_change h: 54 -> 245"},
		{api.Event{Kind: "peer_change", Target: "h", Peer: &api.PeerChange{Old: "10.0.0.1:443", New: "10.0.0.2:443"}}, "peer_change h: 10.0.0.1:443 -> 10.0.0.2:443"},
		{api.Event{Kind: "cert_change", Target: "h\u202e", Cert: &api.CertChange{OldNotAfter: at(1, 2, 3), NewNotAfter: at(4, 5, 6)}},
			"cert_change h: expires 2026-11-22 01:02:03 -> 2026-11-22 04:05:06"},
		{api.Event{Kind: "gap", Gap: &api.Gap{Start: at(14, 0, 0), End: at(14, 0, 9), DurationMs: 9000}}, "gap: no probes until 2026-11-22 14:00:09 (9.000s)"},
		{api.Event{Kind: "watch_started", Started: &api.Started{ExpiresAt: at(16, 0, 0)}}, "watch_started: until 2026-11-22 16:00:00"},
		{api.Event{Kind: "watch_extended", Extended: &api.Extended{OldExpiresAt: at(16, 0, 0), NewExpiresAt: at(17, 0, 0)}},
			"watch_extended: until 2026-11-22 17:00:00, was 2026-11-22 16:00:00"},
		{api.Event{Kind: "watch_stopped", Stopped: &api.Stopped{Reason: "expired"}}, "watch_stopped: expired"},
		{api.Event{Kind: "watch_stopped", Stopped: &api.Stopped{Reason: "failed", Error: "no\nsocket"}}, "watch_stopped: failed, no socket"},
		{api.Event{Kind: "something_new"}, "something_new"},
	} {
		tc.e.Seq, tc.e.Time = 12, at(14, 3, 5).UTC()
		if got, want := eventLine(tc.e), "2026-11-22 14:03:05.120 #12 "+tc.want; got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	}
}

func TestParseFor(t *testing.T) {
	for s, want := range map[string]int64{
		"2h": 7200, "90m": 5400, "1d": 86400, "1d12h": 129600, "7d": 604800, "0d1s": 1, "1h30m5s": 5405,
		"1ms": 1, "1500ms": 2, "1.5h": 5400,
	} {
		if got, err := parseFor(s); err != nil || got != want {
			t.Errorf("%s: %d, %v", s, got, err)
		}
	}
	for _, s := range []string{
		"", "d", "0", "0s", "0d", "5", "1x", "-1h", "+1h", "-1d", "+1d", "1d-1h", "1d+1h", "1dd", "1d1d", "1h1d", "1.5d", "d1h", "1 d", "1d ",
		"99999d", "65535d2000000h", "9999999999h",
	} {
		if got, err := parseFor(s); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", s)) {
			t.Errorf("%q: %d, %v", s, got, err)
		}
	}
}

func TestParseAddress(t *testing.T) {
	for s, want := range map[string]string{
		"unix:///a/b.sock": "/a/b.sock", "unix:///a//b/../c": "/a/c", "unix:///a b%41?c#d": "/a b%41?c#d",
	} {
		if got, err := parseAddress(s); err != nil || got != want {
			t.Errorf("%s: %s, %v", s, got, err)
		}
	}
	for _, s := range []string{
		"", "/a", "a", "unix:", "unix:/a", "unix://", "unix:///", "unix:///..", "unix://a", "unix://host/a", "UNIX:///a",
		"tcp://127.0.0.1:1", "http://localhost", "127.0.0.1:1", " unix:///a", "\x1b[2J",
	} {
		_, err := parseAddress(s)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", s)) {
			t.Errorf("%q: %v", s, err)
		}
	}
}
