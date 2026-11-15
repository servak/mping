package stats

import (
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
)

func icmpTTLEvent(sent time.Time, ttl int) *prober.Event {
	return &prober.Event{
		Key: "h", DisplayName: "h", Result: prober.SUCCESS, SentTime: sent, Rtt: time.Millisecond,
		Details: &prober.ProbeDetails{ProbeType: "icmpv4", ICMP: &prober.ICMPDetails{ReplyTTL: ttl}},
	}
}

func feedEvents(mm MetricsManager, events ...*prober.Event) {
	ch := make(chan *prober.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	<-mm.Subscribe(ch)
}

func replyTTLOf(t *testing.T, mm MetricsManager) ReplyTTLStats {
	t.Helper()
	ms := mm.SortBy(Host, true)
	if len(ms) != 1 {
		t.Fatalf("%d metrics, want 1", len(ms))
	}
	return ms[0].GetReplyTTL()
}

func TestReplyTTLTracking(t *testing.T) {
	mm := NewMetricsManager()
	feedEvents(mm,
		icmpTTLEvent(at(0), 54),
		icmpTTLEvent(at(1), 54),
		icmpTTLEvent(at(2), 0), // unknown: ignored
		&prober.Event{Key: "h", Result: prober.TIMEOUT, SentTime: at(3), Message: "timeout"},
		icmpTTLEvent(at(4), 52),
		icmpTTLEvent(at(5), 52),
		icmpTTLEvent(at(6), 0),
		icmpTTLEvent(at(7), 54),
	)
	s := replyTTLOf(t, mm)
	if s.Last != 54 || s.Min != 52 || s.Max != 54 {
		t.Errorf("last/min/max = %d/%d/%d, want 54/52/54", s.Last, s.Min, s.Max)
	}
	want := []TTLChange{{Time: at(4), Old: 54, New: 52}, {Time: at(7), Old: 52, New: 54}}
	if s.ChangeCount != 2 || len(s.Changes) != 2 {
		t.Fatalf("changes = %+v (count %d), want %+v", s.Changes, s.ChangeCount, want)
	}
	for i, c := range s.Changes {
		if !c.Time.Equal(want[i].Time) || c.Old != want[i].Old || c.New != want[i].New {
			t.Errorf("change %d = %+v, want %+v", i, c, want[i])
		}
	}
	if d := s.Changes[0].HopsDelta(); d != 2 {
		t.Errorf("HopsDelta = %d, want 2", d)
	}
}

func TestReplyTTLNoData(t *testing.T) {
	mm := NewMetricsManager()
	feedEvents(mm, icmpTTLEvent(at(0), 0), icmpTTLEvent(at(1), 0))
	if s := replyTTLOf(t, mm); s.Last != 0 || s.Min != 0 || s.Max != 0 || s.ChangeCount != 0 || len(s.Changes) != 0 {
		t.Errorf("stats without TTLs = %+v", s)
	}
}

func TestReplyTTLChangesCapped(t *testing.T) {
	mm := NewMetricsManager()
	var events []*prober.Event
	n := maxTTLChanges + 5
	for i := 0; i <= n; i++ {
		events = append(events, icmpTTLEvent(at(i), 50+i%2))
	}
	feedEvents(mm, events...)
	s := replyTTLOf(t, mm)
	if s.ChangeCount != n {
		t.Errorf("ChangeCount = %d, want %d", s.ChangeCount, n)
	}
	if len(s.Changes) != maxTTLChanges {
		t.Fatalf("kept %d changes, want %d", len(s.Changes), maxTTLChanges)
	}
	if last := s.Changes[len(s.Changes)-1]; !last.Time.Equal(at(n)) {
		t.Errorf("newest change at %v, want %v", last.Time, at(n))
	}
	if first := s.Changes[0]; !first.Time.Equal(at(n - maxTTLChanges + 1)) {
		t.Errorf("oldest kept change at %v, want %v", first.Time, at(n-maxTTLChanges+1))
	}
}

// Results that arrive out of send order are applied in send order, so a
// late reply does not produce spurious changes.
func TestReplyTTLOutOfOrder(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{SettleTime: time.Hour})
	feedEvents(mm,
		icmpTTLEvent(at(0), 54),
		icmpTTLEvent(at(2), 52),
		icmpTTLEvent(at(1), 54), // late
	)
	s := replyTTLOf(t, mm)
	if s.Last != 52 {
		t.Errorf("Last = %d, want 52 (newest send time)", s.Last)
	}
	if s.ChangeCount != 1 || len(s.Changes) != 1 || !s.Changes[0].Time.Equal(at(2)) {
		t.Errorf("changes = %+v (count %d), want one at %v", s.Changes, s.ChangeCount, at(2))
	}
}

func TestReplyTTLReset(t *testing.T) {
	mm := NewMetricsManager()
	now := time.Now()
	feedEvents(mm, icmpTTLEvent(now.Add(-2*time.Second), 54), icmpTTLEvent(now.Add(-time.Second), 52))
	mm.ResetAllMetrics()
	// A result of a probe sent before the reset is dropped
	feedEvents(mm, icmpTTLEvent(now.Add(-500*time.Millisecond), 60))
	if s := replyTTLOf(t, mm); s.Last != 0 || s.Min != 0 || s.Max != 0 || s.ChangeCount != 0 || len(s.Changes) != 0 {
		t.Errorf("after reset = %+v, want empty", s)
	}
	// The first reply after the reset is not a change
	feedEvents(mm, icmpTTLEvent(time.Now(), 50))
	if s := replyTTLOf(t, mm); s.Last != 50 || s.Min != 50 || s.Max != 50 || s.ChangeCount != 0 {
		t.Errorf("after reset and one reply = %+v", s)
	}
}

// Snapshots must not share the change list with the live metrics.
func TestReplyTTLSnapshotIsolated(t *testing.T) {
	mm := NewMetricsManager()
	feedEvents(mm, icmpTTLEvent(at(0), 54), icmpTTLEvent(at(1), 52))
	before := replyTTLOf(t, mm)
	feedEvents(mm, icmpTTLEvent(at(2), 54))
	if len(before.Changes) != 1 || before.Changes[0].New != 52 {
		t.Errorf("earlier snapshot changed: %+v", before.Changes)
	}
}

// The range covers the replies kept in history, so an old transient TTL
// drops out of it.
func TestReplyTTLRangeIsRecent(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{HistorySize: 5})
	events := []*prober.Event{icmpTTLEvent(at(0), 60)}
	for i := 1; i <= 5; i++ {
		events = append(events, icmpTTLEvent(at(i), 54))
	}
	feedEvents(mm, events[:3]...)
	if s := replyTTLOf(t, mm); s.Min != 54 || s.Max != 60 {
		t.Errorf("min/max with the transient TTL in history = %d/%d, want 54/60", s.Min, s.Max)
	}
	feedEvents(mm, events[3:]...)
	s := replyTTLOf(t, mm)
	if s.Last != 54 || s.Min != 54 || s.Max != 54 {
		t.Errorf("last/min/max = %d/%d/%d, want 54/54/54", s.Last, s.Min, s.Max)
	}
	if s.ChangeCount != 1 {
		t.Errorf("ChangeCount = %d, want 1 (changes outlive the range window)", s.ChangeCount)
	}
}

// httpEvent is an HTTP(S) result; remote/tls empty means no response
func httpEvent(sent time.Time, success bool, remote string, tls *prober.TLSDetails) *prober.Event {
	e := &prober.Event{Key: "h", DisplayName: "h", Result: prober.SUCCESS, SentTime: sent}
	if !success {
		e.Result, e.Message = prober.FAILED, "unexpected status code: 503"
	}
	if remote != "" || tls != nil {
		e.Details = &prober.ProbeDetails{ProbeType: "https", HTTP: &prober.HTTPDetails{StatusCode: 200, RemoteAddr: remote, TLS: tls}}
	}
	return e
}

func TestConnInfo(t *testing.T) {
	mm := NewMetricsManager()
	connOf := func() (ConnInfo, bool) { return mm.SortBy(Host, true)[0].GetConnInfo() }
	feedEvents(mm, httpEvent(at(0), true, "192.0.2.1:443", &prober.TLSDetails{SubjectCN: "old"}))
	ci, ok := connOf()
	if !ok || ci.RemoteAddr != "192.0.2.1:443" || ci.TLS == nil || ci.TLS.SubjectCN != "old" {
		t.Fatalf("after success: %+v, %v", ci, ok)
	}
	// A rejected response (e.g. 503 from the new peer) updates it; a
	// failure without a response and a late older result do not.
	feedEvents(mm,
		httpEvent(at(2), false, "192.0.2.2:443", nil),
		httpEvent(at(3), false, "", nil),
		httpEvent(at(1), true, "192.0.2.1:443", nil),
	)
	ci, ok = connOf()
	if !ok || ci.RemoteAddr != "192.0.2.2:443" || ci.TLS != nil || !ci.SentTime.Equal(at(2)) {
		t.Errorf("after rejected response: %+v, %v", ci, ok)
	}
	mm.ResetAllMetrics()
	if _, ok := connOf(); ok {
		t.Error("reset did not clear the connection info")
	}
}
