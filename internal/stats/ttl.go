package stats

import (
	"time"

	"github.com/servak/mping/internal/prober"
)

// maxTTLChanges is the number of recent reply TTL changes kept per target.
// ReplyTTLStats.ChangeCount keeps counting beyond it.
const maxTTLChanges = 20

// TTLChange is a confirmed change of the reply TTL (IPv4) / hop limit
// (IPv6): the new TTL was seen on enough consecutive successful replies
// (see changeTracker).
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

// ttlChangeTracker keeps the reply TTL changes of a target. It is fed by
// outageTracker, which applies results in send order once they have settled.
type ttlChangeTracker struct {
	tracker changeTracker[uint8]
	last    int // latest known TTL
	changes []TTLChange
	count   int
}

// apply records a reply with a known TTL and returns the change that it
// confirms, if any.
func (t *ttlChangeTracker) apply(r probeResult) (TTLChange, bool) {
	t.last = r.ttl
	vc, ok := t.tracker.observe(uint8(r.ttl), r.sentTime)
	if !ok {
		return TTLChange{}, false
	}
	c := TTLChange{Time: vc.Time, Old: int(vc.Old), New: int(vc.New)}
	t.changes = append(t.changes, c)
	t.count++
	if len(t.changes) > maxTTLChanges {
		t.changes = append([]TTLChange(nil), t.changes[len(t.changes)-maxTTLChanges:]...)
	}
	return c, true
}

// clone returns a copy to read from; it must not be fed, because it does not
// copy the state needed to detect further changes.
func (t *ttlChangeTracker) clone() *ttlChangeTracker {
	if t == nil {
		return nil
	}
	return &ttlChangeTracker{last: t.last, changes: append([]TTLChange(nil), t.changes...), count: t.count}
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
