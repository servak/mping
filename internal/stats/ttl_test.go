package stats

import (
	"math/rand/v2"
	"slices"
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

// ttlSeq returns successful replies with the given TTLs, one per second
// from at(start)
func ttlSeq(start int, ttls ...int) []*prober.Event {
	events := make([]*prober.Event, len(ttls))
	for i, ttl := range ttls {
		events[i] = icmpTTLEvent(at(start+i), ttl)
	}
	return events
}

func checkChanges(t *testing.T, s ReplyTTLStats, want ...TTLChange) {
	t.Helper()
	if s.ChangeCount != len(want) || len(s.Changes) != len(want) {
		t.Fatalf("changes = %+v (count %d), want %+v", s.Changes, s.ChangeCount, want)
	}
	for i, c := range s.Changes {
		if !c.Time.Equal(want[i].Time) || c.Old != want[i].Old || c.New != want[i].New {
			t.Errorf("change %d = %+v, want %+v", i, c, want[i])
		}
	}
}

func TestReplyTTLTracking(t *testing.T) {
	mm := NewMetricsManager()
	events := ttlSeq(0, 54, 54, 52)
	events = append(events,
		icmpTTLEvent(at(3), 0), // unknown: neither advances nor clears the candidate
		&prober.Event{Key: "h", Result: prober.TIMEOUT, SentTime: at(4), Message: "timeout"},
	)
	events = append(events, ttlSeq(5, 52, 52)...)
	// 54 was seen recently, so switching back takes more replies
	events = append(events, ttlSeq(7, repeatTTL(54, ttlChangeMinRepliesRecent)...)...)
	feedEvents(mm, events...)
	s := replyTTLOf(t, mm)
	if s.Last != 54 || s.Min != 52 || s.Max != 54 {
		t.Errorf("last/min/max = %d/%d/%d, want 54/52/54", s.Last, s.Min, s.Max)
	}
	checkChanges(t, s, TTLChange{Time: at(2), Old: 54, New: 52}, TTLChange{Time: at(7), Old: 52, New: 54})
	if d, ok := s.Changes[0].HopsDelta(); d != 2 || !ok {
		t.Errorf("HopsDelta = %d, %v, want 2, true", d, ok)
	}
}

func TestTTLChangeHopsDelta(t *testing.T) {
	tests := []struct {
		old, new, want int
		ok             bool
	}{
		{54, 52, 2, true},
		{114, 118, -4, true},
		{250, 245, 5, true},
		{54, 245, 0, false}, // 64 -> 255
		{120, 60, 0, false}, // 128 -> 64
	}
	for _, tt := range tests {
		if d, ok := (TTLChange{Old: tt.old, New: tt.new}).HopsDelta(); d != tt.want || ok != tt.ok {
			t.Errorf("HopsDelta(%d→%d) = %d, %v, want %d, %v", tt.old, tt.new, d, ok, tt.want, tt.ok)
		}
	}
}

// repeatTTL returns ttl n times
func repeatTTL(ttl, n int) []int {
	return slices.Repeat([]int{ttl}, n)
}

// Per-packet load balancing mixes TTLs at random: runs of 3 are frequent,
// but must not be recorded as a stream of changes.
func TestReplyTTLRandomBalancing(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var tr ttlChangeTracker
	const n = 20000
	for i := range n {
		tr.apply(probeResult{sentTime: at(i), success: true, ttl: 53 + rng.IntN(2)})
	}
	// About one per 2000 replies is expected; runs of 3 would give 1400
	if tr.count > n/500 {
		t.Errorf("%d changes in %d randomly balanced replies", tr.count, n)
	}
}

// The last TTL and the changes stay known when an outage outlasts the history.
func TestReplyTTLSurvivesLongOutage(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{HistorySize: 5})
	events := ttlSeq(0, 54, 52, 52, 52)
	for i := 4; i < 12; i++ {
		events = append(events, &prober.Event{Key: "h", Result: prober.TIMEOUT, SentTime: at(i), Message: "timeout"})
	}
	feedEvents(mm, events...)
	s := replyTTLOf(t, mm)
	if s.Last != 52 || s.Min != 52 || s.Max != 52 {
		t.Errorf("last/min/max = %d/%d/%d, want 52/52/52", s.Last, s.Min, s.Max)
	}
	checkChanges(t, s, TTLChange{Time: at(1), Old: 54, New: 52})
}

func TestReplyTTLChangeNeedsConsecutiveReplies(t *testing.T) {
	tests := []struct {
		name string
		ttls []int
		want []TTLChange
	}{
		{"alternating ECMP paths", []int{54, 53, 54, 53, 54, 53, 54, 53}, nil},
		{"persistent change", []int{54, 54, 52, 52, 52}, []TTLChange{{Time: at(2), Old: 54, New: 52}}},
		{"recent TTL needs a longer run", append([]int{54, 52, 52, 54}, repeatTTL(52, ttlChangeMinRepliesRecent-1)...), nil},
		{"interrupted candidate", append([]int{54, 52, 52, 54}, repeatTTL(52, ttlChangeMinRepliesRecent)...), []TTLChange{{Time: at(4), Old: 54, New: 52}}},
		{"TTL no longer recent", slices.Concat([]int{54, 52}, repeatTTL(54, ttlRecentReplies), repeatTTL(52, 3)), []TTLChange{{Time: at(2 + ttlRecentReplies), Old: 54, New: 52}}},
		{"two short runs of the new TTL", []int{54, 52, 52, 54, 52, 52}, nil},
		{"another candidate replaces it", []int{54, 52, 52, 50, 50, 50}, []TTLChange{{Time: at(3), Old: 54, New: 50}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := NewMetricsManager()
			feedEvents(mm, ttlSeq(0, tt.ttls...)...)
			s := replyTTLOf(t, mm)
			checkChanges(t, s, tt.want...)
		})
	}
	// Alternation stays visible as a range
	mm := NewMetricsManager()
	feedEvents(mm, ttlSeq(0, 54, 53, 54, 53)...)
	if s := replyTTLOf(t, mm); s.Min != 53 || s.Max != 54 {
		t.Errorf("alternating min/max = %d/%d, want 53/54", s.Min, s.Max)
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
	// Blocks of ttlChangeMinReplies replies, each with a new TTL: every
	// block after the first is a change at its first reply.
	n := maxTTLChanges + 5
	var ttls []int
	for k := 0; k <= n; k++ {
		for range ttlChangeMinReplies {
			ttls = append(ttls, 100+k)
		}
	}
	feedEvents(mm, ttlSeq(0, ttls...)...)
	s := replyTTLOf(t, mm)
	if s.ChangeCount != n {
		t.Errorf("ChangeCount = %d, want %d", s.ChangeCount, n)
	}
	if len(s.Changes) != maxTTLChanges {
		t.Fatalf("kept %d changes, want %d", len(s.Changes), maxTTLChanges)
	}
	m := ttlChangeMinReplies
	if last := s.Changes[len(s.Changes)-1]; !last.Time.Equal(at(m * n)) {
		t.Errorf("newest change at %v, want %v", last.Time, at(m*n))
	}
	if first := s.Changes[0]; !first.Time.Equal(at(m * (n - maxTTLChanges + 1))) {
		t.Errorf("oldest kept change at %v, want %v", first.Time, at(m*(n-maxTTLChanges+1)))
	}
}

// Results that arrive out of send order are applied in send order: the
// late 54 interrupts the run of 52s, so no change is recorded (in arrival
// order the 52s would have been consecutive).
func TestReplyTTLOutOfOrder(t *testing.T) {
	// Recent send times, so that nothing settles before the channel closes
	base := time.Now()
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	mm := NewMetricsManagerWithOptions(Options{SettleTime: time.Hour})
	feedEvents(mm,
		icmpTTLEvent(sec(0), 54),
		icmpTTLEvent(sec(1), 52),
		icmpTTLEvent(sec(2), 52),
		icmpTTLEvent(sec(4), 52),
		icmpTTLEvent(sec(3), 54), // late
	)
	s := replyTTLOf(t, mm)
	if s.Last != 52 {
		t.Errorf("Last = %d, want 52 (newest send time)", s.Last)
	}
	checkChanges(t, s)
}

func TestReplyTTLReset(t *testing.T) {
	mm := NewMetricsManager()
	now := time.Now()
	var events []*prober.Event
	for i, ttl := range []int{54, 52, 52, 52} {
		events = append(events, icmpTTLEvent(now.Add(time.Duration(i-5)*time.Second), ttl))
	}
	feedEvents(mm, events...)
	if s := replyTTLOf(t, mm); s.ChangeCount != 1 {
		t.Fatalf("before reset: ChangeCount = %d, want 1", s.ChangeCount)
	}
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
	feedEvents(mm, ttlSeq(0, 54, 52, 52, 52)...)
	before := replyTTLOf(t, mm)
	feedEvents(mm, ttlSeq(4, 50, 50, 50)...)
	if len(before.Changes) != 1 || before.Changes[0].New != 52 {
		t.Errorf("earlier snapshot changed: %+v", before.Changes)
	}
	if s := replyTTLOf(t, mm); s.ChangeCount != 2 {
		t.Errorf("ChangeCount = %d, want 2", s.ChangeCount)
	}
}

// The range covers the replies kept in history, so an old transient TTL
// drops out of it.
func TestReplyTTLRangeIsRecent(t *testing.T) {
	mm := NewMetricsManagerWithOptions(Options{HistorySize: 5})
	events := ttlSeq(0, 60, 54, 54, 54, 54, 54)
	feedEvents(mm, events[:3]...)
	if s := replyTTLOf(t, mm); s.Min != 54 || s.Max != 60 {
		t.Errorf("min/max with the transient TTL in history = %d/%d, want 54/60", s.Min, s.Max)
	}
	feedEvents(mm, events[3:]...)
	s := replyTTLOf(t, mm)
	if s.Last != 54 || s.Min != 54 || s.Max != 54 {
		t.Errorf("last/min/max = %d/%d/%d, want 54/54/54", s.Last, s.Min, s.Max)
	}
	checkChanges(t, s, TTLChange{Time: at(1), Old: 60, New: 54}) // changes outlive the range window
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
