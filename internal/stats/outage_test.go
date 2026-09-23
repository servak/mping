package stats

import (
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// at returns t0 + n seconds (one probe per second)
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

// feed applies results in order; 'S' = success, 'F' = failure, one per second
func feed(tr *outageTracker, pattern string) {
	for i, c := range pattern {
		tr.add(at(i), c == 'S', "timeout", at(i))
	}
}

func TestOutageTracker(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		want      []Outage
		wantCount int
	}{
		{
			name:    "no failures",
			pattern: "SSSS",
		},
		{
			name:    "loss shorter than the threshold is not an outage",
			pattern: "SFFSFS",
		},
		{
			name:      "single outage",
			pattern:   "SFFFS",
			want:      []Outage{{Start: at(1), End: at(4), LostProbes: 3}},
			wantCount: 1,
		},
		{
			name:    "two outages",
			pattern: "FFFSSFFFFS",
			want: []Outage{
				{Start: at(0), End: at(3), LostProbes: 3},
				{Start: at(5), End: at(9), LostProbes: 4},
			},
			wantCount: 2,
		},
		{
			name:      "a success restarts the count",
			pattern:   "FFSFFF",
			want:      []Outage{{Start: at(3), LostProbes: 3}},
			wantCount: 1,
		},
		{
			name:      "ongoing outage",
			pattern:   "SSFFF",
			want:      []Outage{{Start: at(2), LostProbes: 3}},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newOutageTracker(0)
			feed(tr, tt.pattern)
			got := tr.outages()
			if len(got) != len(tt.want) {
				t.Fatalf("got %d outages %+v, want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				g, w := got[i], tt.want[i]
				if !g.Start.Equal(w.Start) || !g.End.Equal(w.End) || g.LostProbes != w.LostProbes {
					t.Errorf("outage[%d] = {%v %v %d}, want {%v %v %d}",
						i, g.Start, g.End, g.LostProbes, w.Start, w.End, w.LostProbes)
				}
			}
			if s := tr.summary(at(10)); s.Count != tt.wantCount {
				t.Errorf("summary count = %d, want %d", s.Count, tt.wantCount)
			}
		})
	}
}

func TestOutageTrackerSummary(t *testing.T) {
	tr := newOutageTracker(0)
	feed(tr, "SFFFSFFFFSFFF") // outages: 3s, 4s, ongoing from t10

	s := tr.summary(at(13)) // ongoing one lasts 3s at t13
	if s.Count != 3 || !s.Ongoing {
		t.Fatalf("summary = %+v", s)
	}
	if s.Total != 10*time.Second {
		t.Errorf("total = %v, want 10s", s.Total)
	}
	if s.Longest != 4*time.Second {
		t.Errorf("longest = %v, want 4s", s.Longest)
	}
	if s := tr.summary(at(18)); s.Longest != 8*time.Second {
		t.Errorf("ongoing outage should become longest: %v", s.Longest)
	}
}

func TestOutageTrackerReordersResults(t *testing.T) {
	// TCP/HTTP probes run concurrently: probes #1-#3 time out after probe
	// #4 already succeeded, so results arrive out of send order.
	settle := 3 * time.Second
	tr := newOutageTracker(settle)

	tr.add(at(0), true, "", at(0))
	tr.add(at(4), true, "", at(4)) // arrives before the timeouts
	tr.add(at(1), false, "timeout", at(4))
	tr.add(at(2), false, "timeout", at(4))
	tr.add(at(3), false, "timeout", at(4))
	tr.add(at(5), true, "", at(5))
	if got := tr.outages(); len(got) != 0 {
		t.Fatalf("results must not be applied before they settle, got %+v", got)
	}

	tr.flush(at(10))
	got := tr.outages()
	if len(got) != 1 || !got[0].Start.Equal(at(1)) || !got[0].End.Equal(at(4)) {
		t.Fatalf("want one outage [t1, t4), got %+v", got)
	}
}

func TestOutageTrackerDropsResultsOlderThanApplied(t *testing.T) {
	tr := newOutageTracker(0)
	tr.add(at(2), true, "", at(2))
	tr.add(at(1), false, "late", at(3)) // older than what was already applied
	if got := tr.outages(); len(got) != 0 {
		t.Errorf("late result must not create an outage, got %+v", got)
	}
}

func TestOutageTrackerReset(t *testing.T) {
	tr := newOutageTracker(0)
	feed(tr, "SFFF")
	tr.reset()
	if s := tr.summary(at(5)); s.Count != 0 || s.Ongoing || len(tr.outages()) != 0 {
		t.Errorf("reset should clear state, got %+v", s)
	}
}

func TestMetricsManagerFlushesOutagesOnClose(t *testing.T) {
	// A large settle window keeps results buffered until the channel closes
	mm := NewMetricsManagerWithOptions(Options{SettleTime: time.Hour})
	mm.ToggleBeep()
	events := make(chan *prober.Event, 10)
	done := mm.Subscribe(events)

	events <- &prober.Event{Key: "h", DisplayName: "h", Result: prober.REGISTER}
	events <- &prober.Event{Key: "h", Result: prober.SUCCESS, SentTime: at(0)}
	for i := 1; i <= 3; i++ {
		events <- &prober.Event{Key: "h", Result: prober.TIMEOUT, SentTime: at(i), Message: "timeout"}
	}
	events <- &prober.Event{Key: "h", Result: prober.SUCCESS, SentTime: at(4)}
	close(events)
	<-done

	ms := mm.SortBy(Host, true)
	if len(ms) != 1 {
		t.Fatalf("expected 1 target, got %d", len(ms))
	}
	got := ms[0].GetOutages()
	if len(got) != 1 || got[0].Duration(at(9)) != 3*time.Second {
		t.Fatalf("want one 3s outage, got %+v", got)
	}
}

func TestSortByReturnsSnapshots(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	mm.ToggleBeep()
	mm.Register("h", "h")
	mm.Failed("h", at(0), "timeout")

	snap := mm.SortBy(Host, true)[0]
	mm.SuccessWithDetails("h", time.Millisecond, at(1), nil)

	if snap.GetSuccessful() != 0 || len(snap.GetRecentHistory(10)) != 1 {
		t.Errorf("snapshot must not observe later updates")
	}
}

func TestRTTPercentiles(t *testing.T) {
	m := newMetrics("h", 200, 0)
	for i := 1; i <= 100; i++ {
		rtt := time.Duration(i) * time.Millisecond
		m.Success(rtt, at(i))
		m.history.AddEntry(HistoryEntry{Timestamp: at(i), RTT: rtt, Success: true})
	}
	m.history.AddEntry(HistoryEntry{Timestamp: at(101), Success: false}) // ignored

	got := m.GetRTTPercentiles(50, 95, 99, 100)
	want := []time.Duration{50 * time.Millisecond, 95 * time.Millisecond, 99 * time.Millisecond, 100 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("p%d: got %v, want %v", []int{50, 95, 99, 100}[i], got[i], want[i])
		}
	}

	empty := newMetrics("e", 10, 0)
	if p := empty.GetRTTPercentiles(95); p[0] != 0 {
		t.Errorf("no samples should give 0, got %v", p[0])
	}
}

func TestSortByDescendingIsStableForTies(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	mm.ToggleBeep()
	for _, h := range []string{"c", "a", "d", "b"} {
		mm.Register(h, h)
	}
	mm.Failed("d", at(0), "timeout")
	mm.SuccessWithDetails("a", 5*time.Millisecond, at(0), nil)

	names := func(ms []Metrics) (s []string) {
		for _, m := range ms {
			s = append(s, m.GetName())
		}
		return
	}
	// Ties keep host name order in both directions, on every call
	for i := 0; i < 20; i++ {
		if got := names(mm.SortBy(Fail, false)); strings.Join(got, ",") != "d,a,b,c" {
			t.Fatalf("Fail desc = %v, want [d a b c]", got)
		}
	}
	// Unmeasured RTT (zero) stays at the end even when descending
	if got := names(mm.SortBy(Avg, false)); got[0] != "a" {
		t.Errorf("Avg desc = %v, want measured host first", got)
	}
}
