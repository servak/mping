package stats

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/servak/mping/internal/prober"
)

// eventLog is an OnEvent hook that keeps the events
type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) add(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

// take returns the events since the last call
func (l *eventLog) take() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events
	l.events = nil
	return events
}

// describe renders an event without its times of recording, with the send
// times as seconds since base
func describe(e Event, base time.Time) string {
	sec := func(t time.Time) int { return int(t.Sub(base) / time.Second) }
	s := fmt.Sprintf("%d %s", sec(e.Time), e.Kind)
	switch {
	case e.Outage != nil && e.Outage.Ongoing():
		s += fmt.Sprintf(" %d- lost %d %s", sec(e.Outage.Start), e.Outage.LostProbes, e.Outage.LastError)
	case e.Outage != nil:
		s += fmt.Sprintf(" %d-%d lost %d %s", sec(e.Outage.Start), sec(e.Outage.End), e.Outage.LostProbes, e.Outage.LastError)
	case e.TTL != nil:
		s += fmt.Sprintf(" %d->%d", e.TTL.Old, e.TTL.New)
	}
	return s
}

func checkEvents(t *testing.T, got []Event, base time.Time, want ...string) {
	t.Helper()
	var s []string
	for _, e := range got {
		s = append(s, describe(e, base))
	}
	if !slices.Equal(s, want) {
		t.Errorf("events:\n got %q\nwant %q", s, want)
	}
}

func newEventManager(settle time.Duration) (*metricsManager, *eventLog) {
	l := &eventLog{}
	mm := NewMetricsManagerWithOptions(Options{SettleTime: settle, OnEvent: l.add})
	return mm.(*metricsManager), l
}

// probes feeds one result per second to target "h" from at(start): '.' is a
// reply without TTL, 'x' a loss and 'a'... a reply with TTL 50, 51, ...
func probes(mm *metricsManager, base time.Time, start int, pattern string) {
	for i, c := range pattern {
		probe(mm, "h", base.Add(time.Duration(start+i)*time.Second), c)
	}
}

func probe(mm *metricsManager, key string, sent time.Time, c rune) {
	switch c {
	case 'x':
		mm.Failed(key, sent, "error at "+sent.UTC().Format("05"))
	case '.':
		mm.SuccessWithDetails(key, time.Millisecond, sent, nil)
	default:
		mm.SuccessWithDetails(key, time.Millisecond, sent, icmpTTLEvent(sent, 50+int(c-'a')).Details)
	}
}

func TestOnEvent(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{"isolated losses", ".xx.x.xx.", nil},
		{"outage", ".xxxx.", []string{
			"1 outage_start 1- lost 3 error at 03",
			"5 outage_end 1-5 lost 4 error at 04",
		}},
		{"outages", "xxx.xxxx..", []string{
			"0 outage_start 0- lost 3 error at 02",
			"3 outage_end 0-3 lost 3 error at 02",
			"4 outage_start 4- lost 3 error at 06",
			"8 outage_end 4-8 lost 4 error at 07",
		}},
		{"TTL change", "aabbbb", []string{"2 ttl_change 50->51"}},
		{"alternating TTLs", "abababab", nil},
		{"TTL change in an outage", "aabxxxbb", []string{
			"3 outage_start 3- lost 3 error at 05",
			"6 outage_end 3-6 lost 3 error at 05",
			"2 ttl_change 50->51",
		}},
		{"TTL change confirmed as the outage ends", "aabbxxxb", []string{
			"4 outage_start 4- lost 3 error at 06",
			"2 ttl_change 50->51",
			"7 outage_end 4-7 lost 3 error at 06",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm, log := newEventManager(0)
			mm.autoRegister("h", "display name", "icmpv4")
			before := time.Now()
			probes(mm, t0, 0, tt.pattern)
			events := log.take()
			checkEvents(t, events, t0, tt.want...)
			for _, e := range events {
				if e.Key != "h" || e.Name != "display name" {
					t.Errorf("key, name = %q, %q", e.Key, e.Name)
				}
				if e.RecordedAt.Before(before) || e.RecordedAt.After(time.Now()) {
					t.Errorf("RecordedAt = %v", e.RecordedAt)
				}
			}
		})
	}
}

// Only the loss that makes the outage is an event.
func TestOnEventOutageStartOnce(t *testing.T) {
	mm, log := newEventManager(0)
	for i, want := range []int{0, 0, 1, 0, 0} {
		probes(mm, t0, i, "x")
		if got := log.take(); len(got) != want {
			t.Errorf("loss %d: %d events, want %d", i+1, len(got), want)
		}
	}
}

// Results that arrive out of send order are judged in send order.
func TestOnEventSendOrder(t *testing.T) {
	base := time.Now()
	type result struct {
		at int
		c  rune
	}
	tests := []struct {
		name    string
		arrival []result
		want    []string
	}{
		{"late reply interrupts the losses", []result{{0, 'x'}, {1, 'x'}, {3, 'x'}, {2, '.'}}, nil},
		{"late loss completes the outage", []result{{0, 'x'}, {1, 'x'}, {3, '.'}, {2, 'x'}},
			[]string{"0 outage_start", "3 outage_end"}},
		{"late reply interrupts the run", []result{{0, 'a'}, {1, 'b'}, {2, 'b'}, {4, 'b'}, {3, 'a'}}, nil},
		{"late reply completes the run", []result{{0, 'a'}, {1, 'b'}, {2, 'b'}, {4, 'a'}, {3, 'b'}},
			[]string{"1 ttl_change"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm, log := newEventManager(time.Hour) // nothing settles before the flush
			for _, r := range tt.arrival {
				probe(mm, "h", base.Add(time.Duration(r.at)*time.Second), r.c)
			}
			if got := log.take(); len(got) != 0 {
				t.Errorf("%d events before anything settled", len(got))
			}
			mm.flushOutages()
			var got []string
			for _, e := range log.take() {
				got = append(got, fmt.Sprintf("%d %s", e.Time.Sub(base)/time.Second, e.Kind))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// An event is reported with the first result after the one that tells it has
// settled, and what is left when the channel closes is reported before done.
func TestOnEventSettling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mm, log := newEventManager(2500 * time.Millisecond)
		base := time.Now()
		ch := make(chan *prober.Event)
		done := mm.Subscribe(ch)
		second := func(r *prober.Event) {
			r.Key, r.SentTime = "h", time.Now()
			ch <- r
			synctest.Wait()
			time.Sleep(time.Second)
		}
		lost := func() { second(&prober.Event{Result: prober.TIMEOUT, Message: "timeout"}) }
		reply := func() { second(&prober.Event{Result: prober.SUCCESS}) }
		for range 5 {
			lost() // sent at 0, 1, 2, 3, 4
		}
		if got := log.take(); len(got) != 0 {
			t.Errorf("the loss at 2 cannot be settled at 4: %d events", len(got))
		}
		reply() // 5: the loss at 2 is settled
		checkEvents(t, log.take(), base, "0 outage_start 0- lost 3 timeout")
		reply()
		reply()
		if got := log.take(); len(got) != 0 {
			t.Errorf("the reply at 5 cannot be settled at 7: %d events", len(got))
		}
		// Results stop
		time.Sleep(time.Hour)
		if got := log.take(); len(got) != 0 {
			t.Errorf("%d events without results", len(got))
		}
		close(ch)
		<-done
		checkEvents(t, log.take(), base, "5 outage_end 0-5 lost 5 timeout")
	})
}

// Reading settles results, but it does not call the hook, nor does what it
// settles get lost or reported twice.
func TestOnEventReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mm, log := newEventManager(time.Second)
		base := time.Now()
		probes(mm, base, 0, "xxx")
		time.Sleep(time.Minute)
		for range 3 {
			ms := mm.SortBy(Host, true)
			if o, ok := ms[0].GetLastOutage(); !ok || !o.Ongoing() {
				t.Fatalf("reading does not see the outage: %+v, %v", o, ok)
			}
			mm.GetMetrics("h")
			// A snapshot is complete in itself and reports nothing
			s := ms[0].(*metrics)
			s.outage.add(base.Add(time.Minute), true, "", time.Now())
			s.outage.flushAll()
		}
		if got := log.take(); len(got) != 0 {
			t.Errorf("reading called the hook: %d events", len(got))
		}
		probe(mm, "other", time.Now(), '.') // any target
		checkEvents(t, log.take(), base, "0 outage_start 0- lost 3 error at 02")

		mm.SortBy(Host, true)
		mm.flushOutages()
		mm.flushOutages()
		if got := log.take(); len(got) != 0 {
			t.Errorf("%d events repeated", len(got))
		}
	})
}

func TestOnEventFlushAfterReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mm, log := newEventManager(time.Second)
		base := time.Now()
		ch := make(chan *prober.Event)
		done := mm.Subscribe(ch)
		for i := range 3 {
			ch <- &prober.Event{Key: "h", Result: prober.TIMEOUT, SentTime: base.Add(time.Duration(i) * time.Second), Message: "timeout"}
		}
		time.Sleep(time.Minute)
		mm.SortBy(Host, true)
		close(ch)
		<-done
		checkEvents(t, log.take(), base, "0 outage_start 0- lost 3 timeout")
	})
}

// The hook may read metrics, whatever path it is called from.
func TestOnEventRunsOutsideLock(t *testing.T) {
	for _, settle := range []time.Duration{0, time.Hour} { // with a result; on close
		var mm *metricsManager
		calls := 0
		mm = NewMetricsManagerWithOptions(Options{SettleTime: settle, OnEvent: func(e Event) {
			calls++
			if !mm.mu.TryLock() {
				t.Error("the hook is called with the lock held")
				return
			}
			mm.mu.Unlock()
			if _, ok := mm.SortBy(Host, true)[0].GetLastOutage(); !ok {
				t.Error("the outage is not visible to the hook")
			}
		}}).(*metricsManager)
		ch := make(chan *prober.Event, 4)
		for i, r := range []*prober.Event{{Result: prober.TIMEOUT}, {Result: prober.FAILED}, {Result: prober.TIMEOUT}, {Result: prober.SUCCESS}} {
			r.Key, r.SentTime = "h", time.Now().Add(time.Duration(i-10)*time.Second)
			ch <- r
		}
		close(ch)
		<-mm.Subscribe(ch)
		if calls != 2 {
			t.Errorf("settle %v: %d calls, want 2", settle, calls)
		}
	}
}

func TestOnEventReset(t *testing.T) {
	tests := []struct {
		name          string
		before, after string // one result per second; the reset is between
		want          []string
	}{
		{"ongoing outage recovers", "xxx", "...", nil},
		{"ongoing outage stays", "xxxx", "xxx.", []string{
			"4 outage_start 4- lost 3 error at 06", "7 outage_end 4-7 lost 3 error at 06"}},
		{"run of losses", "xx", "xx.", nil},
		{"TTL change candidate", "abb", "bbbb", nil},
		{"first TTL is no change", "aaa", "bbbb", nil},
		{"recent TTLs", "ababab", "bbbaaa", []string{"9 ttl_change 51->50"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mm, log := newEventManager(0)
				base := time.Now()
				second := func(c rune) {
					probe(mm, "h", time.Now(), c)
					time.Sleep(time.Second)
				}
				for _, c := range tt.before {
					second(c)
				}
				log.take()
				mm.ResetAllMetrics()
				for _, c := range tt.after {
					second(c)
				}
				mm.flushOutages()
				checkEvents(t, log.take(), base, tt.want...)
			})
		})
	}
}

// What was only pending, or established but not reported yet, when metrics
// are reset is never reported.
func TestOnEventResetDropsUnreported(t *testing.T) {
	for _, read := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			mm, log := newEventManager(time.Second)
			base := time.Now()
			probes(mm, base, 0, "xxx")
			time.Sleep(time.Minute)
			if read {
				mm.SortBy(Host, true) // establishes the outage
			}
			mm.ResetAllMetrics()
			probe(mm, "h", base.Add(4*time.Second), '.') // sent before the reset
			probe(mm, "h", time.Now(), '.')
			mm.flushOutages()
			if got := log.take(); len(got) != 0 {
				t.Errorf("read %v: %d events from before the reset", read, len(got))
			}
		})
	}
}

// ResetAllMetrics returns only when the hook is not running anymore.
func TestOnEventResetWaitsForHook(t *testing.T) {
	inHook, leave := make(chan struct{}), make(chan struct{})
	var left atomic.Bool
	mm := NewMetricsManagerWithOptions(Options{OnEvent: func(Event) {
		close(inHook)
		<-leave
		left.Store(true)
	}}).(*metricsManager)
	go probes(mm, t0, 0, "xxx")
	<-inHook
	reset := make(chan bool)
	go func() {
		mm.ResetAllMetrics()
		reset <- left.Load()
	}()
	select {
	case <-reset:
		t.Fatal("reset returned while the hook was running")
	case <-time.After(20 * time.Millisecond):
	}
	close(leave)
	if !<-reset {
		t.Error("reset returned before the hook")
	}
}

// Without the hook nothing is collected.
func TestNoEventHook(t *testing.T) {
	mm := NewMetricsManager().(*metricsManager)
	probes(mm, t0, 0, "aabbbbxxxx.")
	mm.SortBy(Host, true)
	mm.flushOutages()
	if len(mm.events) != 0 {
		t.Errorf("%d events queued", len(mm.events))
	}
}

// Results of many targets, readers and (in half of the runs) resets at once:
// every target reports its facts once and in order, and none from before a
// reset after it.
func TestOnEventConcurrent(t *testing.T) {
	for _, resets := range []bool{false, true} {
		t.Run(fmt.Sprintf("resets=%v", resets), func(t *testing.T) { testOnEventConcurrent(t, resets) })
	}
}

func testOnEventConcurrent(t *testing.T, resets bool) {
	const (
		subscribers = 3
		targets     = 4 // per subscriber
		rounds      = 100
		pattern     = "aaaabbbbcccc" + "xxxx" // 2 or 3 TTL changes and an outage
	)
	var (
		hooks     atomic.Int32
		resetDone atomic.Int64 // start (unix ns) of the latest reset that has returned
		mu        sync.Mutex
		byTarget  = map[string][]Event{}
	)
	mm := NewMetricsManagerWithOptions(Options{SettleTime: 200 * time.Microsecond, OnEvent: func(e Event) {
		if hooks.Add(1) != 1 {
			t.Error("concurrent calls of the hook")
		}
		defer hooks.Add(-1)
		if e.Time.UnixNano() < resetDone.Load() {
			t.Errorf("event from before the reset: %+v", e)
		}
		mu.Lock()
		defer mu.Unlock()
		byTarget[e.Key] = append(byTarget[e.Key], e)
	}}).(*metricsManager)

	var results, resetting, reading sync.WaitGroup
	stop, halfway := make(chan struct{}), make(chan struct{})
	repeat := func(wg *sync.WaitGroup, stop <-chan struct{}, pause time.Duration, f func()) {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				case <-time.After(pause):
					f()
				}
			}
		})
	}
	for range 3 {
		repeat(&reading, stop, 100*time.Microsecond, func() { mm.SortBy(Host, true) })
	}
	var firstHalf sync.WaitGroup
	firstHalf.Add(subscribers * targets)
	if resets {
		// In the first half only, so that every target has events to report
		repeat(&resetting, halfway, 2*time.Millisecond, func() {
			start := time.Now().UnixNano()
			mm.ResetAllMetrics()
			resetDone.Store(start)
		})
	}
	for s := range subscribers {
		ch := make(chan *prober.Event, 100)
		done := mm.Subscribe(ch)
		var feeders sync.WaitGroup
		for k := range targets {
			key := fmt.Sprintf("s%d-t%d", s, k)
			feeders.Go(func() {
				var sent time.Time
				for round := range rounds {
					if round == rounds/2 {
						firstHalf.Done()
						<-halfway
						resetting.Wait()
					}
					for _, c := range pattern {
						// Distinct, whatever the resolution of the clock
						if now := time.Now(); now.Sub(sent) > time.Microsecond {
							sent = now
						} else {
							sent = sent.Add(time.Microsecond)
						}
						e := &prober.Event{Key: key, Result: prober.TIMEOUT, SentTime: sent, Message: "timeout"}
						if c != 'x' {
							e = icmpTTLEvent(sent, 50+int(c-'a'))
							e.Key = key
						}
						ch <- e
						if resets {
							time.Sleep(10 * time.Microsecond)
						}
					}
				}
				ch <- &prober.Event{Key: key, Result: prober.SUCCESS, SentTime: sent.Add(time.Millisecond)}
			})
		}
		results.Go(func() {
			feeders.Wait()
			close(ch)
			<-done
		})
	}
	firstHalf.Wait()
	close(halfway)
	results.Wait()
	close(stop)
	reading.Wait()

	for key, events := range byTarget {
		var last Event
		var current *Outage
		seen := map[string]bool{}
		starts, ends, sinceReset := 0, 0, 0
		m := mm.GetMetrics(key)
		for _, e := range events {
			id := fmt.Sprint(e.Kind, e.Time.UnixNano())
			if seen[id] {
				t.Errorf("%s: %s reported twice", key, e.Kind)
			}
			seen[id] = true
			if e.RecordedAt.Before(last.RecordedAt) {
				t.Errorf("%s: %s recorded before the event before it", key, e.Kind)
			}
			last = e
			switch e.Kind {
			case EventOutageStart:
				starts++
				if e.RecordedAt.After(m.(*metrics).resetAt) {
					sinceReset++
				}
				if current != nil && !resets {
					t.Errorf("%s: outage started twice", key)
				}
				if current != nil && !e.Time.After(current.Start) {
					t.Errorf("%s: outages out of order", key)
				}
				current = e.Outage
			case EventOutageEnd:
				ends++
				if current == nil || !current.Start.Equal(e.Outage.Start) {
					t.Errorf("%s: end of an outage that has not started: %+v", key, e.Outage)
				}
				current = nil
			}
		}
		if !resets && (starts != rounds || ends != rounds) {
			t.Errorf("%s: %d starts and %d ends, want %d", key, starts, ends, rounds)
		}
		if n := m.GetOutageSummary().Count; sinceReset != n || n < rounds/2 {
			t.Errorf("%s: %d outages reported since the last reset, want %d", key, sinceReset, n)
		}
		if !resets && len(events) != starts+ends+m.GetReplyTTL().ChangeCount {
			t.Errorf("%s: %d events, want %d outages and %d TTL changes", key, len(events), rounds, m.GetReplyTTL().ChangeCount)
		}
	}
	if len(byTarget) != subscribers*targets {
		t.Errorf("events of %d targets, want %d", len(byTarget), subscribers*targets)
	}
}
