// Package icmpid allocates ICMP echo identifiers shared by every ICMP user
// in the process: the ICMP probers (internal/prober) and the path tracers
// (internal/trace).
//
// Raw ICMP sockets receive the echo replies of every socket on the host, so
// the echo ID is what tells the replies of one prober or tracer apart from
// another's. Drawing all IDs from one allocator guarantees that the IDs held
// at the same time within a process are distinct.
//
// IDs are handed out by a cursor that starts at a random per-process base, so
// concurrent mping processes rarely pick the same IDs (PID-derived IDs
// overlap for neighbouring PIDs). ID 0 is never returned. A released ID is
// quarantined for a while before it can be handed out again, so that late
// replies addressed to its previous owner are not taken for the new owner's.
package icmpid

import (
	"errors"
	"math/rand/v2"
	"sync"
	"time"
)

// DefaultQuarantine is how long a released ID stays unavailable.
const DefaultQuarantine = 10 * time.Second

// ErrExhausted is returned by Acquire when every ID is held or quarantined.
var ErrExhausted = errors.New("icmpid: no free ICMP echo ID (all held or recently released)")

// Allocator hands out distinct non-zero 16-bit echo IDs. The zero value is
// not usable; create one with New.
type Allocator struct {
	mu         sync.Mutex
	quarantine time.Duration
	now        func() time.Time
	cursor     uint16
	held       map[uint16]struct{}
	released   map[uint16]time.Time // quarantined IDs and when they were released
}

// New returns an allocator whose cursor starts at base and which keeps
// released IDs out of circulation for quarantine. A nil now means time.Now.
func New(base uint16, quarantine time.Duration, now func() time.Time) *Allocator {
	if now == nil {
		now = time.Now
	}
	return &Allocator{
		quarantine: quarantine,
		now:        now,
		cursor:     base,
		held:       make(map[uint16]struct{}),
		released:   make(map[uint16]time.Time),
	}
}

var global = New(uint16(rand.Uint32()), DefaultQuarantine, nil)

// Acquire takes an ID from the process-wide allocator. See Allocator.Acquire.
func Acquire() (*Lease, error) { return global.Acquire() }

// Lease is the ownership of one echo ID. Only the lease can release the ID,
// and releasing is idempotent, so a double Release never affects a later
// owner of the same ID.
type Lease struct {
	a    *Allocator
	id   uint16
	once sync.Once
}

// ID returns the echo ID the lease holds.
func (l *Lease) ID() uint16 { return l.id }

// Release gives the ID back; it then stays quarantined before it can be
// reissued. Only the first call has an effect.
func (l *Lease) Release() {
	l.once.Do(func() { l.a.release(l.id) })
}

// Acquire returns a lease on a non-zero ID that no one holds and that was not
// released within the quarantine. The cursor walks the 16-bit space from the
// base and wraps around. If no ID qualifies it returns ErrExhausted.
func (a *Allocator) Acquire() (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for range 1 << 16 {
		id := a.cursor
		a.cursor++
		if id == 0 {
			continue
		}
		if _, ok := a.held[id]; ok {
			continue
		}
		if at, ok := a.released[id]; ok {
			if now.Sub(at) < a.quarantine {
				continue
			}
			delete(a.released, id)
		}
		a.held[id] = struct{}{}
		return &Lease{a: a, id: id}, nil
	}
	return nil, ErrExhausted
}

func (a *Allocator) release(id uint16) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.held, id)
	a.released[id] = a.now()
}
