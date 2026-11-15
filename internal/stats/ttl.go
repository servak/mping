package stats

import (
	"time"

	"github.com/servak/mping/internal/prober"
)

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

// ReplyTTLStats describes the reply TTLs / hop limits of a target. Last,
// Min and Max are 0 when no recent reply carried a known TTL.
type ReplyTTLStats struct {
	Last int // TTL of the reply to the most recently sent probe
	// Min and Max cover the replies kept in history (recent probes only)
	Min int
	Max int
	// Changes holds the most recent changes (at most maxTTLChanges),
	// oldest first; ChangeCount counts all of them.
	Changes     []TTLChange
	ChangeCount int
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
