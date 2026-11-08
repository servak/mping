package icmpid

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func mustAcquire(t *testing.T, a *Allocator) *Lease {
	t.Helper()
	l, err := a.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// IDs are never 0, even when the cursor passes through it.
func TestAcquireNeverZero(t *testing.T) {
	a := New(0xfff0, 0, nil)
	for range 100 {
		l := mustAcquire(t, a)
		if l.ID() == 0 {
			t.Fatal("Acquire() returned 0")
		}
		l.Release()
	}
	if l := mustAcquire(t, New(0, 0, nil)); l.ID() == 0 {
		t.Fatal("Acquire() with base 0 returned 0")
	}
}

// Every non-zero ID can be held at once, each exactly once; then Acquire
// fails instead of duplicating, and succeeds again after a release.
func TestAcquireFullSpaceThenExhausted(t *testing.T) {
	a := New(0x1234, 0, nil)
	seen := make(map[uint16]bool)
	var leases []*Lease
	for range 0xffff {
		l := mustAcquire(t, a)
		if l.ID() == 0 || seen[l.ID()] {
			t.Fatalf("Acquire() = %#x: zero or duplicate", l.ID())
		}
		seen[l.ID()] = true
		leases = append(leases, l)
	}
	if a.Held() != 0xffff {
		t.Fatalf("Held() = %d, want 65535", a.Held())
	}
	if _, err := a.Acquire(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("Acquire() on a full allocator: err = %v, want ErrExhausted", err)
	}
	leases[7].Release()
	if l := mustAcquire(t, a); l.ID() != leases[7].ID() {
		t.Fatalf("Acquire() = %#x, want the released %#x", l.ID(), leases[7].ID())
	}
}

// Quarantined IDs count as unavailable for exhaustion too.
func TestExhaustedByQuarantine(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	a := New(1, time.Second, clk.now)
	var leases []*Lease
	for range 0xffff {
		leases = append(leases, mustAcquire(t, a))
	}
	for _, l := range leases {
		l.Release()
	}
	if _, err := a.Acquire(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("err = %v, want ErrExhausted while all IDs are quarantined", err)
	}
	clk.advance(time.Second)
	mustAcquire(t, a)
}

// Long-lived holders keep their IDs while short-lived holders churn through
// and wrap around the whole space many times.
func TestWrapSkipsHeldIDs(t *testing.T) {
	a := New(0xabcd, 0, nil)
	prober, other := mustAcquire(t, a), mustAcquire(t, a)
	for range 5 * 0x10000 {
		l := mustAcquire(t, a)
		if l.ID() == prober.ID() || l.ID() == other.ID() || l.ID() == 0 {
			t.Fatalf("Acquire() = %#x, collides with a held ID", l.ID())
		}
		l.Release()
	}
	if a.Held() != 2 {
		t.Fatalf("Held() = %d, want 2", a.Held())
	}
}

// A released ID is not reissued within the quarantine, and is after it.
func TestQuarantine(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	a := New(1, 10*time.Second, clk.now)
	l := mustAcquire(t, a)
	id := l.ID()
	l.Release()
	// Hold every other ID: the only candidate left is the quarantined one.
	for range 0xfffe {
		if got := mustAcquire(t, a); got.ID() == id {
			t.Fatalf("released ID %#x reissued within quarantine", id)
		}
	}
	clk.advance(9 * time.Second)
	if _, err := a.Acquire(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("err = %v, want ErrExhausted within quarantine", err)
	}
	clk.advance(time.Second)
	if got := mustAcquire(t, a); got.ID() != id {
		t.Fatalf("Acquire() = %#x, want %#x after quarantine", got.ID(), id)
	}
}

// Reviewer repro: an ID held across a full wrap and then released must not be
// handed out by the very next Acquire.
func TestHeldAcrossWrapNotReissued(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	a := New(1, 10*time.Second, clk.now)
	p := mustAcquire(t, a)
	for range 0xffff - 1 {
		mustAcquire(t, a).Release()
		clk.advance(time.Millisecond)
	}
	p.Release()
	for range 3 {
		if got := mustAcquire(t, a); got.ID() == p.ID() {
			t.Fatalf("released ID %#x reissued within quarantine", got.ID())
		}
	}
}

// A second Release on a lease is a no-op: it must not release the ID again
// for a later owner.
func TestDoubleReleaseKeepsNewOwner(t *testing.T) {
	a := New(1, 0, nil)
	first := mustAcquire(t, a)
	first.Release()
	// Hold everything: with no quarantine the first ID goes to a new owner.
	var second *Lease
	for range 0xffff {
		if l := mustAcquire(t, a); l.ID() == first.ID() {
			second = l
		}
	}
	if second == nil {
		t.Fatal("ID not reissued")
	}
	first.Release()
	if a.Held() != 0xffff {
		t.Fatalf("stale Release changed Held() to %d", a.Held())
	}
	if _, err := a.Acquire(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("live lease %#x was reissued after a stale Release: err = %v", second.ID(), err)
	}
}

// Concurrent acquirers get distinct IDs (run with -race).
func TestAcquireConcurrent(t *testing.T) {
	a := New(0x8000, DefaultQuarantine, nil)
	const workers, per = 16, 1000
	ids := make(chan uint16, workers*per)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range per {
				l, err := a.Acquire()
				if err != nil {
					t.Error(err)
					return
				}
				ids <- l.ID()
				if i%2 == 0 {
					l.Release()
				}
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := make(map[uint16]bool)
	for id := range ids {
		if id == 0 || seen[id] {
			t.Fatalf("Acquire() = %#x: zero or duplicate", id)
		}
		seen[id] = true
	}
	if a.Held() != workers*per/2 {
		t.Fatalf("Held() = %d, want %d", a.Held(), workers*per/2)
	}
}

// The process-wide Acquire shares one allocator.
func TestGlobal(t *testing.T) {
	a, err := Acquire()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	defer b.Release()
	if a.ID() == 0 || b.ID() == 0 || a.ID() == b.ID() {
		t.Fatalf("Acquire() = %#x, %#x", a.ID(), b.ID())
	}
}
