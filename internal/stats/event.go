package stats

import (
	"time"

	"github.com/servak/mping/internal/prober"
)

// EventKind is what an Event reports
type EventKind string

const (
	EventOutageStart EventKind = "outage_start" // Event.Outage
	EventOutageEnd   EventKind = "outage_end"   // Event.Outage
	EventTTLChange   EventKind = "ttl_change"   // Event.TTL
	EventPeerChange  EventKind = "peer_change"  // Event.Peer
	EventCertChange  EventKind = "cert_change"  // Event.Cert
)

// Event is a fact about a target, see Options.OnEvent. It shares nothing with
// the manager, so it may be kept and changed.
type Event struct {
	Kind EventKind
	Key  string // target key
	Name string // display name of the target
	// Time is when it happened: the send time of the first lost probe of an
	// outage, of the reply that ended it, or of the first reply with the new
	// value of a change. All of them are known only later, so the Time of an
	// event may be before that of the event before it (e.g. a change whose
	// replies were interrupted by an outage).
	Time time.Time
	// RecordedAt is when the fact was established: when the last of the
	// results that tell it was applied.
	RecordedAt time.Time

	// The one that Kind names is set, the others are nil.

	// Outage is the outage as of the event: at its start End is zero and
	// LostProbes and LastError cover the losses that made it an outage.
	Outage *Outage
	TTL    *TTLChange
	Peer   *PeerChange
	Cert   *CertChange
}

// PeerChange is a confirmed change of the address (IP:port) that answers an
// HTTP(S) target. Like a TTLChange it takes several consecutive responses,
// and more of them for a recent address, so that DNS round robin is not a
// stream of changes. Every response counts, also a rejected one (e.g. a 503).
// Probes that failed without a response change nothing.
type PeerChange struct {
	Old, New string
}

// CertChange is a confirmed change of the leaf certificate of an HTTPS
// target, following the rules of PeerChange. A certificate is identified by
// subject, issuer and expiry. The TLS version belongs to the connection and
// is left empty. Plain HTTP responses (e.g. after a redirect) tell nothing
// about the certificate and change nothing: they neither end the old
// certificate nor interrupt the run of a new one. Their address counts for
// PeerChange, though.
type CertChange struct {
	Old, New prober.TLSDetails
}

// certificate returns what identifies the certificate of a connection; ok is
// false for plain HTTP and if nothing is known about it.
func certificate(d *prober.TLSDetails) (cert prober.TLSDetails, ok bool) {
	if d == nil {
		return cert, false
	}
	// UTC without monotonic clock, so that == compares the instant
	cert = prober.TLSDetails{SubjectCN: d.SubjectCN, Issuer: d.Issuer, NotAfter: d.NotAfter.Round(0).UTC()}
	return cert, cert != prober.TLSDetails{}
}

// eventQueue collects the events of all targets in the order in which they
// were established, for dispatchEvents. It is guarded by the manager's lock.
type eventQueue []Event

// emitter returns the function with which the tracker of a target reports
// its events. It must be called with the manager's lock held.
func (q *eventQueue) emitter(key, name string) func(Event) {
	return func(e Event) {
		e.Key, e.Name, e.RecordedAt = key, name, time.Now()
		*q = append(*q, e)
	}
}

// dispatchEvents calls the hook for the queued events. eventMu is held
// meanwhile, so that concurrent calls cannot reorder events and
// ResetAllMetrics can wait for the hook; mm.mu is not, so that the hook may
// read metrics.
func (mm *metricsManager) dispatchEvents() {
	if mm.onEvent == nil {
		return
	}
	mm.eventMu.Lock()
	defer mm.eventMu.Unlock()

	mm.mu.Lock()
	events := mm.events
	mm.events = nil
	mm.mu.Unlock()
	for _, e := range events {
		mm.onEvent(e)
	}
}
