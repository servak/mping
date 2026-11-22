package stats

import (
	"fmt"
	"testing"
)

// The values of a tracker are bounded however many different ones it sees,
// and reusing their ids does not mix them up.
func TestChangeTrackerReusesIDs(t *testing.T) {
	var tr changeTracker[string]
	sent := 0
	observe := func(v string) (valueChange[string], bool) {
		sent++
		return tr.observe(v, at(sent))
	}
	observe("first")
	for i := range 1000 {
		if c, ok := observe(fmt.Sprint(i)); ok {
			t.Fatalf("change without a run: %+v", c)
		}
	}
	if len(tr.values) > changeRecentReplies+2 {
		t.Errorf("%d values kept, want at most %d", len(tr.values), changeRecentReplies+2)
	}
	// prev has not been seen for long, but is still known
	for i := range changeMinReplies {
		c, ok := observe("new")
		if want := i == changeMinReplies-1; ok != want {
			t.Fatalf("reply %d: change = %v, want %v", i, ok, want)
		}
		if ok && (c.Old != "first" || c.New != "new" || !c.Time.Equal(at(sent-changeMinReplies+1))) {
			t.Errorf("change = %+v", c)
		}
	}
}

// Values that rotate or alternate, like the addresses of DNS round robin,
// are no changes: neither a strict rotation, nor one that gives each address
// a few consecutive replies.
func TestChangeTrackerRotation(t *testing.T) {
	for _, pool := range []int{2, 3, 10, 40} {
		for run := 1; run < changeMinRepliesRecent && pool*run <= changeRecentReplies; run++ {
			var tr changeTracker[string]
			changes := 0
			for i := range 1000 {
				// The first round is not repeated yet, so runs of
				// changeMinReplies count there.
				if _, ok := tr.observe(fmt.Sprint(i/run%pool), at(i)); ok && i >= pool*run {
					changes++
				}
			}
			if changes != 0 {
				t.Errorf("pool of %d, runs of %d: %d changes", pool, run, changes)
			}
		}
	}
}
