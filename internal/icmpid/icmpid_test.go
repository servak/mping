package icmpid

import (
	"sync"
	"testing"
)

// IDs are never 0, even when the counter passes through it.
func TestAcquireNeverZero(t *testing.T) {
	a := New(0xfff0)
	for range 100 {
		id := a.Acquire()
		if id == 0 {
			t.Fatal("Acquire() returned 0")
		}
		a.Release(id)
	}
	if id := New(0).Acquire(); id == 0 {
		t.Fatal("Acquire() with base 0 returned 0")
	}
}

// Every non-zero ID can be held at once, each exactly once.
func TestAcquireDistinctFullSpace(t *testing.T) {
	a := New(0x1234)
	seen := make(map[uint16]bool)
	for range 0xffff {
		id := a.Acquire()
		if id == 0 || seen[id] {
			t.Fatalf("Acquire() = %#x: zero or duplicate", id)
		}
		seen[id] = true
	}
	if a.Held() != 0xffff {
		t.Fatalf("Held() = %d, want 65535", a.Held())
	}
	// Exhausted: an ID is still returned (documented duplicate), never 0
	if id := a.Acquire(); id == 0 {
		t.Fatal("Acquire() on a full allocator returned 0")
	}
}

// A long-lived holder (a prober) keeps its ID while short-lived holders
// (tracers restarted on every target change) churn through and wrap around
// the whole space many times.
func TestWrapSkipsHeldIDs(t *testing.T) {
	a := New(0xabcd)
	prober := a.Acquire()
	other := a.Acquire()
	for range 5 * 0x10000 {
		id := a.Acquire()
		if id == prober || id == other || id == 0 {
			t.Fatalf("Acquire() = %#x, collides with a held ID", id)
		}
		a.Release(id)
	}
	if a.Held() != 2 {
		t.Fatalf("Held() = %d, want 2", a.Held())
	}
}

// A released ID is not reused before the counter wraps around.
func TestReleaseNotReusedImmediately(t *testing.T) {
	a := New(1)
	id := a.Acquire()
	a.Release(id)
	for range 1000 {
		if got := a.Acquire(); got == id {
			t.Fatalf("released ID %#x reused before wraparound", id)
		}
	}
	a.Release(0x4242) // not held: no-op
}

// Concurrent acquirers get distinct IDs; released ones are not reused
// before the wraparound either (run with -race).
func TestAcquireConcurrent(t *testing.T) {
	a := New(0x8000)
	const workers, per = 16, 1000
	ids := make(chan uint16, workers*per)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range per {
				id := a.Acquire()
				ids <- id
				if i%2 == 0 {
					a.Release(id)
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

// The process-wide functions share one allocator.
func TestGlobal(t *testing.T) {
	a, b := Acquire(), Acquire()
	if a == 0 || b == 0 || a == b {
		t.Fatalf("Acquire() = %#x, %#x", a, b)
	}
	if !InUse(a) || !InUse(b) {
		t.Fatal("acquired IDs not in use")
	}
	Release(a)
	Release(b)
	if InUse(a) || InUse(b) {
		t.Fatal("released IDs still in use")
	}
}
