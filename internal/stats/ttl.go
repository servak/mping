package stats

import (
	"slices"
	"time"

	"github.com/servak/mping/internal/prober"
)

// maxTTLChanges is the number of recent reply TTL changes kept per target.
// ReplyTTLStats.ChangeCount keeps counting beyond it.
const maxTTLChanges = 20

// TTLChange is a confirmed change of the reply TTL (IPv4) / hop limit
// (IPv6): the new TTL was seen on enough consecutive successful replies
// (see ttlChangeMinReplies).
type TTLChange struct {
	Time time.Time // send time of the first reply with the new TTL
	Old  int
	New  int
}

// HopsDelta returns the change of the estimated return path length,
// positive when the reply crossed more hops. ok is false when the two TTLs
// imply different initial TTLs (e.g. 54 and 245): then another kind of
// responder probably answers, and the difference says nothing about hops.
func (c TTLChange) HopsDelta() (hops int, ok bool) {
	oldHops, okOld := prober.EstimateHops(c.Old)
	newHops, okNew := prober.EstimateHops(c.New)
	if !okOld || !okNew || c.Old+oldHops != c.New+newHops {
		return 0, false
	}
	return newHops - oldHops, true
}

// ReplyTTLStats describes the reply TTLs / hop limits of a target. Last,
// Min and Max are 0 when no reply carried a known TTL.
type ReplyTTLStats struct {
	Last int // TTL of the most recent reply, however long ago
	// Min and Max cover the replies kept in history (recent probes only);
	// both equal Last when history holds no reply (a long outage)
	Min int
	Max int
	// Changes holds the most recent changes (at most maxTTLChanges),
	// oldest first; ChangeCount counts all of them.
	Changes     []TTLChange
	ChangeCount int
}

const (
	// ttlChangeMinReplies is the number of consecutive successful replies a
	// new TTL must be seen on before it is recorded as a change (like the 3
	// consecutive losses that make an outage).
	ttlChangeMinReplies = 3

	// ttlChangeMinRepliesRecent applies instead when the TTL was already
	// seen among the ttlRecentReplies replies before the run. Per-packet
	// load balancing over unequal paths mixes TTLs at random, where runs of
	// 3 are common (every 14 replies for an even split) but runs of 10 are
	// rare. A real switch back to a recent path is still recorded, only
	// later.
	ttlChangeMinRepliesRecent = 10
	ttlRecentReplies          = 50
)

// ttlChangeTracker detects TTL changes between successful replies. It is
// fed by outageTracker, which applies results in send order once they have
// settled, so a late reply cannot fake a change.
//
// prev is the confirmed TTL. A reply with another TTL starts (or extends)
// a candidate; once the candidate's TTL has been seen on candNeed
// consecutive replies it is recorded as a change at the send time of its
// first reply. A reply with the confirmed TTL drops the candidate. Failures
// and replies with an unknown TTL change nothing.
type ttlChangeTracker struct {
	prev     int // confirmed TTL; 0 until the first reply with a known TTL
	candTTL  int // candidate TTL; 0 if none
	candSent time.Time
	candRun  int
	candNeed int // candRun needed to record the candidate
	changes  []TTLChange
	count    int

	recent [ttlRecentReplies]uint8 // ring of the latest TTLs; 0 = unused
	next   int                     // next slot of recent
	last   int                     // latest known TTL
}

func (t *ttlChangeTracker) apply(r probeResult) {
	if !r.success || r.ttl <= 0 || r.ttl > 255 {
		return
	}
	t.detect(r)
	t.last = r.ttl
	t.recent[t.next] = uint8(r.ttl)
	t.next = (t.next + 1) % len(t.recent)
}

// detect applies a reply to prev and the candidate; recent still holds only
// the replies before it.
func (t *ttlChangeTracker) detect(r probeResult) {
	switch {
	case t.prev == 0:
		t.prev = r.ttl // the first known TTL is not a change
		return
	case r.ttl == t.prev:
		t.candTTL, t.candRun = 0, 0
		return
	case r.ttl == t.candTTL:
		t.candRun++
	default:
		t.candTTL, t.candSent, t.candRun = r.ttl, r.sentTime, 1
		t.candNeed = ttlChangeMinReplies
		if slices.Contains(t.recent[:], uint8(r.ttl)) {
			t.candNeed = ttlChangeMinRepliesRecent
		}
	}
	if t.candRun < t.candNeed {
		return
	}
	t.changes = append(t.changes, TTLChange{Time: t.candSent, Old: t.prev, New: t.candTTL})
	t.count++
	if len(t.changes) > maxTTLChanges {
		t.changes = append([]TTLChange(nil), t.changes[len(t.changes)-maxTTLChanges:]...)
	}
	t.prev = t.candTTL
	t.candTTL, t.candRun = 0, 0
}

func (t ttlChangeTracker) clone() ttlChangeTracker {
	t.changes = append([]TTLChange(nil), t.changes...)
	return t
}

// ConnInfo is the peer of the latest response received from an HTTP(S)
// target, successful or not (e.g. a 503 during a switchover).
type ConnInfo struct {
	SentTime   time.Time // send time of the probe that got the response
	RemoteAddr string
	TLS        *prober.TLSDetails // nil for plain HTTP; shared, read-only
}

// add records the connection info of details if it is newer (by send
// time, ties go to the later result, like LastRTT).
func (c *ConnInfo) add(sentTime time.Time, details *prober.ProbeDetails) {
	if details == nil || details.HTTP == nil {
		return
	}
	h := details.HTTP
	if h.RemoteAddr == "" && h.TLS == nil {
		return
	}
	if sentTime.Before(c.SentTime) {
		return
	}
	*c = ConnInfo{SentTime: sentTime, RemoteAddr: h.RemoteAddr, TLS: h.TLS}
}
