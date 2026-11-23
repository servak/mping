package stats

import (
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"
)

func succeed(m *metrics, i int, rtt time.Duration) {
	m.Success(rtt, at(i))
	m.history.AddEntry(HistoryEntry{Timestamp: at(i), RTT: rtt, Success: true})
}

func TestRTTBuckets(t *testing.T) {
	last := -1
	for rtt := time.Duration(1); rtt < time.Hour; rtt += 1 + rtt/100 {
		i := rttBucket(rtt)
		if i != last && i != last+1 {
			t.Fatalf("%v: bucket %d after %d", rtt, i, last)
		}
		last = i
		if rtt <= histogramMin {
			continue
		}
		if off := math.Abs(float64(rttOfBucket(i))/float64(rtt) - 1); off > 0.025 {
			t.Fatalf("%v: bucket %d of %v is %.2f%% off", rtt, i, rttOfBucket(i), 100*off)
		}
	}
	if a, b := rttBucket(time.Second)+1, rttBucket(time.Minute)+1; a > 290 || b > 375 {
		t.Errorf("%d buckets up to a second, %d up to a minute", a, b)
	}
	for _, rtt := range []time.Duration{-1, 0, 1, histogramMin} {
		if i := rttBucket(rtt); i != 0 {
			t.Errorf("%v: bucket %d", rtt, i)
		}
	}
}

// However many probes there were, the percentiles are those of all of them.
func TestRTTPercentilesOfAll(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	lognormal := func(median time.Duration, sigma float64) time.Duration {
		return time.Duration(float64(median) * math.Exp(sigma*r.NormFloat64()))
	}
	for name, next := range map[string]func(i int) time.Duration{
		"lan": func(int) time.Duration { return lognormal(400*time.Microsecond, 0.3) },
		"slow at first": func(i int) time.Duration {
			if i < 3000 {
				return lognormal(80*time.Millisecond, 0.2)
			}
			return lognormal(7*time.Millisecond, 0.1)
		},
		"two paths": func(i int) time.Duration {
			return lognormal([]time.Duration{20, 35}[i%2]*time.Millisecond, 0.02)
		},
		"constant":  func(int) time.Duration { return 1234567 },
		"very fast": func(int) time.Duration { return lognormal(5*time.Microsecond, 0.3) },
	} {
		m := newMetrics(name, 100, 0)
		var all []time.Duration
		for i := range 7200 {
			rtt := next(i)
			all = append(all, rtt)
			succeed(m, i, rtt)
		}
		slices.Sort(all)
		ps := []float64{0, 1, 25, 50, 75, 95, 99, 100}
		for i, got := range m.snapshot().GetRTTPercentiles(ps...) {
			want := all[max(int(math.Ceil(ps[i]/100*7200)), 1)-1]
			if off := math.Abs(float64(got)/float64(want) - 1); off > 0.025 {
				t.Errorf("%s: p%v is %v, want %v", name, ps[i], got, want)
			}
		}
	}
}

func TestRTTPercentilesExactAtFirst(t *testing.T) {
	m := newMetrics("h", 100, 0)
	for i := 1; i <= 100; i++ {
		succeed(m, i, time.Duration(i)*time.Millisecond)
	}
	if got := m.GetRTTPercentiles(95)[0]; got != 95*time.Millisecond {
		t.Errorf("p95 of 100 is %v", got)
	}
	// The first one is no longer in the history, with a failure as with a success
	m.history.AddEntry(HistoryEntry{Timestamp: at(101)})
	for p, want := range map[float64]time.Duration{1: time.Millisecond, 50: 50 * time.Millisecond, 100: 100 * time.Millisecond} {
		got := m.GetRTTPercentiles(p)[0]
		if off := math.Abs(float64(got)/float64(want) - 1); off > 0.025 {
			t.Errorf("p%v is %v, want %v", p, got, want)
		}
	}
}

func TestRTTPercentilesResetAndSnapshot(t *testing.T) {
	m := newMetrics("h", 10, 0)
	for i := range 50 {
		succeed(m, i, 500*time.Millisecond)
	}
	snap := m.snapshot()
	m.Reset()
	if got := m.GetRTTPercentiles(50)[0]; got != 0 || m.rtts.total != 0 {
		t.Errorf("p50 after a reset is %v of %d", got, m.rtts.total)
	}
	for i := range 50 {
		succeed(m, 100+i, time.Millisecond)
	}
	if got := m.GetRTTPercentiles(100)[0]; got != time.Millisecond {
		t.Errorf("p100 after a reset is %v", got)
	}
	snap2 := m.snapshot()
	succeed(m, 200, time.Millisecond)
	for range 1000 {
		m.rtts.add(time.Millisecond)
	}
	if got := snap.GetRTTPercentiles(0)[0]; got != 500*time.Millisecond {
		t.Errorf("p0 of the earlier snapshot is %v", got)
	}
	if c := snap2.rtts.counts[rttBucket(time.Millisecond)]; c != 50 {
		t.Errorf("a snapshot has changed: %d", c)
	}
}
