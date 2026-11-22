package stats

import (
	"sort"
	"time"

	"github.com/servak/mping/internal/prober"
)

const (
	// maxStoredOutages bounds memory for long-running, flapping targets.
	// Aggregates (count, total, longest) keep counting beyond this limit.
	maxStoredOutages = 1000

	// outageMinLostProbes is the number of consecutive lost probes that make
	// an outage. Shorter runs are treated as isolated packet loss.
	outageMinLostProbes = 3
)

// Outage is a period during which a target was unreachable.
//
// outageMinLostProbes consecutive lost probes make an outage and the next
// successful probe ends it. Start is the send time of the first lost probe of
// the run and End is the send time of that successful probe, so Duration is
// "lost probes × interval" with a precision of one interval.
type Outage struct {
	Start      time.Time
	End        time.Time // zero while the outage is ongoing
	LostProbes int
	LastError  string
}

// Ongoing reports whether the outage has not recovered yet
func (o Outage) Ongoing() bool {
	return o.End.IsZero()
}

// Duration returns the outage length; ongoing outages are measured up to now
func (o Outage) Duration(now time.Time) time.Duration {
	if o.Ongoing() {
		return now.Sub(o.Start)
	}
	return o.End.Sub(o.Start)
}

// OutageSummary aggregates all outages of a target
type OutageSummary struct {
	Count   int
	Total   time.Duration
	Longest time.Duration
	Ongoing bool
}

// probeResult is a settled or pending probe outcome used for ordering
type probeResult struct {
	sentTime time.Time
	success  bool
	err      string
	ttl      int // reply TTL / hop limit of a success, 0 if unknown
	// http is the response of an HTTP(S) probe, accepted or not; nil if none
	http *prober.HTTPDetails
}

// outageTracker detects outages for one target.
//
// Probe results can arrive out of send order (e.g. a timeout is reported after
// a later probe already succeeded), so results are held for `settle` and
// applied in send-time order once no earlier result can still arrive.
// Changes of the reply TTL, the HTTP(S) peer and its certificate are detected
// on the same ordered stream.
type outageTracker struct {
	settle time.Duration

	pending []probeResult // sorted by sentTime
	applied time.Time     // sentTime of the newest applied result

	lostRun []probeResult // consecutive failures not yet making an outage

	current *Outage
	closed  []Outage

	count   int
	total   time.Duration
	longest time.Duration

	ttl *ttlChangeTracker // nil until the first reply with a known TTL

	emit func(Event) // reports events; nil = off, as in clones

	// Only for events, unlike ttl; nil until the first response that tells
	peer *changeTracker[string]
	cert *changeTracker[prober.TLSDetails]
}

func newOutageTracker(settle time.Duration) *outageTracker {
	return &outageTracker{settle: settle}
}

// addResult records a probe result and applies every result that has settled
func (t *outageTracker) addResult(r probeResult, now time.Time) {
	if r.sentTime.IsZero() {
		r.sentTime = now
	}
	sentTime := r.sentTime
	i := sort.Search(len(t.pending), func(i int) bool {
		return t.pending[i].sentTime.After(sentTime)
	})
	t.pending = append(t.pending, probeResult{})
	copy(t.pending[i+1:], t.pending[i:])
	t.pending[i] = r
	t.flush(now)
}

// flush applies pending results sent before now-settle
func (t *outageTracker) flush(now time.Time) {
	t.flushUntil(now.Add(-t.settle))
}

// flushAll applies every pending result (used when no more results can arrive)
func (t *outageTracker) flushAll() {
	t.flushUntil(time.Time{})
}

func (t *outageTracker) flushUntil(deadline time.Time) {
	n := 0
	for _, r := range t.pending {
		if !deadline.IsZero() && r.sentTime.After(deadline) {
			break
		}
		// A result that arrives after newer ones were applied can't be placed
		// in order anymore; drop it from outage detection only.
		if !r.sentTime.Before(t.applied) {
			t.apply(r)
			t.applied = r.sentTime
		}
		n++
	}
	t.pending = t.pending[n:]
}

func (t *outageTracker) apply(r probeResult) {
	t.applyTTL(r)
	if r.http != nil && t.emit != nil {
		t.applyConn(r.http, r.sentTime)
	}
	switch {
	case r.success:
		t.lostRun = t.lostRun[:0]
		if t.current != nil {
			t.closeCurrent(r.sentTime)
		}
	case t.current != nil:
		t.current.LostProbes++
		t.current.LastError = r.err
	default:
		t.lostRun = append(t.lostRun, r)
		if len(t.lostRun) >= outageMinLostProbes {
			t.current = &Outage{Start: t.lostRun[0].sentTime, LostProbes: len(t.lostRun), LastError: r.err}
			t.count++
			t.lostRun = t.lostRun[:0]
			t.emitOutage(EventOutageStart, t.current.Start, *t.current)
		}
	}
}

// applyTTL applies the reply TTL of r. Failures and unknown TTLs change
// nothing.
func (t *outageTracker) applyTTL(r probeResult) {
	if !r.success || r.ttl <= 0 || r.ttl > 255 {
		return
	}
	if t.ttl == nil {
		t.ttl = &ttlChangeTracker{}
	}
	if c, ok := t.ttl.apply(r); ok && t.emit != nil {
		ev := c // only what is reported goes to the heap
		t.emit(Event{Kind: EventTTLChange, Time: ev.Time, TTL: &ev})
	}
}

// applyConn applies the peer and the certificate of a response
func (t *outageTracker) applyConn(h *prober.HTTPDetails, sentTime time.Time) {
	if h.RemoteAddr != "" {
		if t.peer == nil {
			t.peer = &changeTracker[string]{}
		}
		if c, ok := t.peer.observe(h.RemoteAddr, sentTime); ok {
			t.emit(Event{Kind: EventPeerChange, Time: c.Time, Peer: &PeerChange{Old: c.Old, New: c.New}})
		}
	}
	if cert, ok := certificate(h.TLS); ok {
		if t.cert == nil {
			t.cert = &changeTracker[prober.TLSDetails]{}
		}
		if c, ok := t.cert.observe(cert, sentTime); ok {
			t.emit(Event{Kind: EventCertChange, Time: c.Time, Cert: &CertChange{Old: c.Old, New: c.New}})
		}
	}
}

func (t *outageTracker) emitOutage(kind EventKind, at time.Time, o Outage) {
	if t.emit != nil {
		ev := o // only what is reported goes to the heap
		t.emit(Event{Kind: kind, Time: at, Outage: &ev})
	}
}

func (t *outageTracker) closeCurrent(end time.Time) {
	o := *t.current
	o.End = end
	d := o.Duration(end)
	t.total += d
	if d > t.longest {
		t.longest = d
	}
	t.closed = append(t.closed, o)
	if len(t.closed) > maxStoredOutages {
		t.closed = append([]Outage(nil), t.closed[len(t.closed)-maxStoredOutages:]...)
	}
	t.current = nil
	t.emitOutage(EventOutageEnd, o.End, o)
}

// outages returns a copy of closed outages plus the ongoing one, oldest first
func (t *outageTracker) outages() []Outage {
	res := make([]Outage, 0, len(t.closed)+1)
	res = append(res, t.closed...)
	if t.current != nil {
		res = append(res, *t.current)
	}
	return res
}

// last returns the ongoing outage, or else the most recent closed one
func (t *outageTracker) last() (Outage, bool) {
	if t.current != nil {
		return *t.current, true
	}
	if len(t.closed) > 0 {
		return t.closed[len(t.closed)-1], true
	}
	return Outage{}, false
}

// summary aggregates outages; an ongoing outage is measured up to now
func (t *outageTracker) summary(now time.Time) OutageSummary {
	s := OutageSummary{Count: t.count, Total: t.total, Longest: t.longest}
	if t.current != nil {
		d := t.current.Duration(now)
		s.Total += d
		if d > s.Longest {
			s.Longest = d
		}
		s.Ongoing = true
	}
	return s
}

// clone returns a copy to read from; it reports no events
func (t *outageTracker) clone() *outageTracker {
	c := *t
	c.emit, c.peer, c.cert = nil, nil, nil
	c.pending = append([]probeResult(nil), t.pending...)
	c.lostRun = append([]probeResult(nil), t.lostRun...)
	c.ttl = t.ttl.clone()
	// closed is append-only and trimming reallocates it, so the clone can
	// share the backing array as long as its capacity is capped.
	c.closed = t.closed[:len(t.closed):len(t.closed)]
	if t.current != nil {
		cur := *t.current
		c.current = &cur
	}
	return &c
}

func (t *outageTracker) reset() {
	emit := t.emit
	*t = *newOutageTracker(t.settle)
	t.emit = emit
}
