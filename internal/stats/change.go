package stats

import (
	"slices"
	"time"
)

const (
	// changeMinReplies is the number of consecutive replies a new value must
	// be seen on before it is recorded as a change (like the 3 consecutive
	// losses that make an outage).
	changeMinReplies = 3

	// changeMinRepliesRecent applies instead when the value was already seen
	// among the changeRecentReplies replies before the run. Load balancing
	// (per packet over unequal paths, or by DNS round robin) mixes values at
	// random, where runs of 3 are common (every 14 replies for an even
	// split) but runs of 10 are rare. A real switch back to a recent value
	// is still recorded, only later.
	changeMinRepliesRecent = 10
	changeRecentReplies    = 50
)

// valueChange is a confirmed change of a value tracked by a changeTracker
type valueChange[T any] struct {
	Time     time.Time // send time of the first reply with the new value
	Old, New T
}

// changeTracker detects changes of a value carried by the replies of one
// target (reply TTL, peer, certificate). It must be fed in send order by
// outageTracker, so that a late reply cannot fake a change. Replies that do
// not tell the value must not be fed; they change nothing.
//
// prev is the confirmed value. A reply with another value starts (or
// extends) a candidate; once the candidate has been seen on candNeed
// consecutive replies it is recorded as a change at the send time of its
// first reply. A reply with the confirmed value drops the candidate.
//
// Values are referred to by a small id, so that the replies to remember take
// a byte each whatever T is.
type changeTracker[T comparable] struct {
	// values[id-1] is the value with that id. Ids that are no longer
	// referred to are reused, which bounds it to the distinct values of
	// recent plus prev and the candidate.
	values []T

	recent [changeRecentReplies]uint8 // ring of the latest ids; 0 = unused
	next   uint8                      // next slot of recent

	prev     uint8 // id of the confirmed value; 0 until the first reply
	cand     uint8 // id of the candidate; 0 if none
	candRun  uint8
	candNeed uint8 // candRun needed to record the candidate
	candSent time.Time
}

// observe applies a reply with value v and returns the change that it
// confirms, if any.
func (t *changeTracker[T]) observe(v T, sentTime time.Time) (valueChange[T], bool) {
	id := t.id(v)
	c, ok := t.detect(id, sentTime)
	t.recent[t.next] = id
	t.next = (t.next + 1) % changeRecentReplies
	return c, ok
}

// detect applies a reply to prev and the candidate; recent still holds only
// the replies before it.
func (t *changeTracker[T]) detect(id uint8, sentTime time.Time) (c valueChange[T], ok bool) {
	switch id {
	case t.prev:
		t.cand, t.candRun = 0, 0
		return c, false
	case t.cand:
		t.candRun++
	default:
		if t.prev == 0 {
			t.prev = id // the first known value is not a change
			return c, false
		}
		t.cand, t.candSent, t.candRun = id, sentTime, 1
		t.candNeed = changeMinReplies
		if slices.Contains(t.recent[:], id) {
			t.candNeed = changeMinRepliesRecent
		}
	}
	if t.candRun < t.candNeed {
		return c, false
	}
	c = valueChange[T]{Time: t.candSent, Old: t.values[t.prev-1], New: t.values[t.cand-1]}
	t.prev = t.cand
	t.cand, t.candRun = 0, 0
	return c, true
}

// id returns the id of v, a new or reused one if v has none yet
func (t *changeTracker[T]) id(v T) uint8 {
	if i := slices.Index(t.values, v); i >= 0 {
		return uint8(i + 1)
	}
	for i := range t.values {
		if id := uint8(i + 1); id != t.prev && id != t.cand && !slices.Contains(t.recent[:], id) {
			t.values[i] = v
			return id
		}
	}
	t.values = append(t.values, v)
	return uint8(len(t.values))
}
