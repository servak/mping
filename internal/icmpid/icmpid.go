// Package icmpid allocates ICMP echo identifiers shared by every ICMP user
// in the process: the ICMP probers (internal/prober) and the path tracers
// (internal/trace).
//
// Raw ICMP sockets receive the echo replies of every socket on the host, so
// the echo ID is what tells the replies of one prober or tracer apart from
// another's. Drawing all IDs from one allocator guarantees that the IDs held
// at the same time within a process are distinct.
//
// IDs count up from a random per-process base, so concurrent mping processes
// rarely pick the same IDs (PID-derived IDs overlap for neighbouring PIDs).
// ID 0 is never returned. Released IDs become available again, but only
// after the counter wraps around the 16-bit space, so a just-released ID is
// not handed out again while late replies to it may still arrive.
package icmpid

import (
	"math/rand/v2"
	"sync"
)

// Allocator hands out distinct non-zero 16-bit echo IDs. The zero value is
// not usable; create one with New.
type Allocator struct {
	mu   sync.Mutex
	base uint16
	next uint32 // offset from base of the next candidate
	used map[uint16]struct{}
}

// New returns an allocator whose IDs count up from base.
func New(base uint16) *Allocator {
	return &Allocator{base: base, used: make(map[uint16]struct{})}
}

var global = New(uint16(rand.Uint32()))

// Acquire returns an ID from the process-wide allocator. See
// Allocator.Acquire.
func Acquire() uint16 { return global.Acquire() }

// Release returns id to the process-wide allocator. See Allocator.Release.
func Release(id uint16) { global.Release(id) }

// InUse reports whether id is held in the process-wide allocator.
func InUse(id uint16) bool { return global.InUse(id) }

// Acquire returns a non-zero ID that no other caller holds. The counter
// walks the 16-bit space from the base and wraps around, skipping 0 and the
// IDs still held, so up to 65535 IDs can be held at once. If all of them
// are held (far more than a process creates), the next counter value is
// returned anyway and may duplicate a held ID.
func (a *Allocator) Acquire() uint16 {
	a.mu.Lock()
	defer a.mu.Unlock()
	var fallback uint16
	for range 1 << 16 {
		id := a.base + uint16(a.next)
		a.next++
		if id == 0 {
			continue
		}
		if fallback == 0 {
			fallback = id
		}
		if _, held := a.used[id]; !held {
			a.used[id] = struct{}{}
			return id
		}
	}
	return fallback
}

// Release marks id as no longer held, once its socket is closed. Releasing
// an ID that is not held is a no-op.
func (a *Allocator) Release(id uint16) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.used, id)
}

// InUse reports whether id is currently held.
func (a *Allocator) InUse(id uint16) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, held := a.used[id]
	return held
}

// Held returns the number of IDs currently held.
func (a *Allocator) Held() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.used)
}
