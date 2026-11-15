package stats

import "time"

// maxTTLChanges is the number of recent reply TTL changes kept per target.
// ReplyTTLStats.ChangeCount keeps counting beyond it.
const maxTTLChanges = 20

// TTLChange is a change of the reply TTL (IPv4) / hop limit (IPv6) between
// two consecutive successful replies, in send order.
type TTLChange struct {
	Time time.Time // send time of the probe whose reply had the new TTL
	Old  int
	New  int
}

// HopsDelta returns the change of the estimated return path length: Old -
// New, positive when the reply crossed more hops. It assumes the responder's
// initial TTL did not change.
func (c TTLChange) HopsDelta() int {
	return c.Old - c.New
}

// ReplyTTLStats describes the reply TTLs / hop limits of a target. All
// values are 0 (and Changes empty) when no reply carried a known TTL.
type ReplyTTLStats struct {
	Last int // TTL of the reply to the most recently sent probe
	Min  int
	Max  int
	// Changes holds the most recent changes (at most maxTTLChanges),
	// oldest first; ChangeCount counts all of them.
	Changes     []TTLChange
	ChangeCount int
}

// ttlRange tracks the last, minimum and maximum reply TTL. It is updated
// as results arrive: newest send time wins for Last, like LastRTT.
type ttlRange struct {
	last     int
	lastSent time.Time
	min, max int
}

func (r *ttlRange) add(ttl int, sentTime time.Time) {
	if ttl <= 0 {
		return
	}
	if r.last == 0 || !sentTime.Before(r.lastSent) {
		r.last, r.lastSent = ttl, sentTime
	}
	if r.min == 0 || ttl < r.min {
		r.min = ttl
	}
	if ttl > r.max {
		r.max = ttl
	}
}

// ttlChangeTracker detects TTL changes between consecutive successful
// replies. It is fed by outageTracker, which applies results in send order
// once they have settled, so a late reply cannot fake a change.
type ttlChangeTracker struct {
	prev    int // TTL of the previous successful reply with a known TTL
	changes []TTLChange
	count   int
}

func (t *ttlChangeTracker) apply(r probeResult) {
	if !r.success || r.ttl <= 0 {
		return
	}
	if t.prev != 0 && r.ttl != t.prev {
		t.changes = append(t.changes, TTLChange{Time: r.sentTime, Old: t.prev, New: r.ttl})
		t.count++
		if len(t.changes) > maxTTLChanges {
			t.changes = append([]TTLChange(nil), t.changes[len(t.changes)-maxTTLChanges:]...)
		}
	}
	t.prev = r.ttl
}

func (t ttlChangeTracker) clone() ttlChangeTracker {
	t.changes = append([]TTLChange(nil), t.changes...)
	return t
}
