package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/session"
	"github.com/servak/mping/internal/stats"
)

// fakeSession is a session whose results and end are up to the test.
type fakeSession struct {
	params  session.Params
	metrics stats.MetricsManager
	results chan *prober.Event
	settled <-chan struct{}
	fail    chan error    // ends the session by itself
	gate    chan struct{} // closed when the session may end
	once    sync.Once
}

func (f *fakeSession) Start(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		select {
		case <-ctx.Done():
		case err := <-f.fail:
			if err != nil {
				errCh <- err
			}
		}
		<-f.gate
		f.Stop()
	}()
	return errCh
}

func (f *fakeSession) Stop() {
	f.once.Do(func() { close(f.results) })
	<-f.settled
}

func (f *fakeSession) Metrics() stats.MetricsManager { return f.metrics }

// probe reports the result of a probe sent now.
func (f *fakeSession) probe(host, errMsg string) {
	r := &prober.Event{Key: host, DisplayName: host, SentTime: time.Now()}
	if errMsg != "" {
		r.Result, r.Message = prober.FAILED, errMsg
	} else {
		r.Result = prober.SUCCESS
	}
	f.results <- r
}

func (f *fakeSession) probes(host, errMsg string, n int) {
	for range n {
		time.Sleep(time.Second)
		f.probe(host, errMsg)
	}
}

// env is a manager with fake sessions. Its wall clock can be moved.
type env struct {
	*Manager
	t *testing.T

	mu       sync.Mutex
	jump     time.Duration // of the wall clock
	sessions []*fakeSession
	opened   int

	config func() (*config.Config, error)
	open   func(ctx context.Context, f *fakeSession) error // before the session is there
	held   bool                                            // new sessions end only when their gate is closed
}

func newEnv(t *testing.T, limits Limits) *env {
	e := &env{t: t}
	now := clock()
	e.Manager = newManager(func() (*config.Config, error) {
		if e.config != nil {
			return e.config()
		}
		return config.DefaultConfig(), nil
	}, e.newFake, limits, func() instant {
		e.mu.Lock()
		defer e.mu.Unlock()
		i := now()
		i.wall = i.wall.Add(e.jump)
		return i
	})
	return e
}

func (e *env) newFake(ctx context.Context, p session.Params) (Session, error) {
	f := &fakeSession{params: p, results: make(chan *prober.Event), fail: make(chan error), gate: make(chan struct{})}
	if e.open != nil {
		if err := e.open(ctx, f); err != nil {
			return nil, err
		}
	}
	if !e.held {
		close(f.gate)
	}
	f.metrics = stats.NewMetricsManagerWithOptions(p.Stats)
	f.settled = f.metrics.Subscribe(f.results)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.opened++
	e.sessions = append(e.sessions, f)
	return f, nil
}

func (e *env) setJump(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.jump = d
}

// end shuts the manager down, which a bubble requires.
func (e *env) end() {
	for _, f := range e.sessions {
		select {
		case <-f.gate:
		default:
			close(f.gate)
		}
	}
	if err := e.Shutdown(context.Background()); err != nil {
		e.t.Error(err)
	}
}

// bubble runs f in a synctest bubble with a manager.
func bubble(t *testing.T, limits Limits, f func(t *testing.T, e *env)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, limits)
		defer e.end()
		f(t, e)
	})
}

func request(name string, targets ...string) api.CreateRequest {
	if len(targets) == 0 {
		targets = []string{"a"}
	}
	return api.CreateRequest{Name: name, Targets: targets}
}

// start creates a watch and returns it with its session.
func (e *env) start(req api.CreateRequest) (api.Watch, *fakeSession) {
	e.t.Helper()
	w, err := e.Create(context.Background(), req)
	if err != nil {
		e.t.Fatal(err)
	}
	synctest.Wait()
	return w, e.sessions[len(e.sessions)-1]
}

func (e *env) state(id string) (string, string) {
	e.t.Helper()
	w, err := e.Get(id)
	if err != nil {
		e.t.Fatal(err)
	}
	return w.State, w.Reason
}

func (e *env) wantState(id, state, reason string) {
	e.t.Helper()
	if s, r := e.state(id); s != state || r != reason {
		e.t.Errorf("%s is %s (%s), want %s (%s)", id, s, r, state, reason)
	}
}

func (e *env) events(id string) []api.Event {
	e.t.Helper()
	events, err := e.Events(id, 0, 100000)
	if err != nil {
		e.t.Fatal(err)
	}
	for i, ev := range events.Events {
		if ev.Seq != int64(i+1) {
			e.t.Errorf("event %d has number %d", i+1, ev.Seq)
		}
	}
	return events.Events
}

func (e *env) wantKinds(id string, want ...string) {
	e.t.Helper()
	var kinds []string
	for _, ev := range e.events(id) {
		kinds = append(kinds, ev.Kind)
	}
	if got := strings.Join(kinds, " "); got != strings.Join(want, " ") {
		e.t.Errorf("events: %s\nwant %s", got, strings.Join(want, " "))
	}
}

func (e *env) summary(id string, all bool) api.Summary {
	e.t.Helper()
	s, err := e.Summary(id, all)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Errorf("got error %v (%#v), want code %s", err, err, code)
	}
}

func TestLifecycle(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.held = true
		req := request("w", "a", "h[1-2]", "h1")
		req.SourceInterface = "eth0"
		req.TimeoutMs = 2500
		w, f := e.start(req)
		if w.State != api.StateRunning || w.TargetCount != 3 || w.Counts.Pending != 3 || w.ExpiresAt.Sub(w.StartedAt) != 2*time.Hour {
			t.Errorf("created %+v", w)
		}
		if got := fmt.Sprint(f.params.Targets, f.params.Interval, f.params.Timeout, f.params.SourceInterface); got != "[a h1 h2] 1s 2.5seth0" {
			t.Errorf("session has %s", got)
		}
		if d, _ := e.Get(w.ID); fmt.Sprint(d.Targets) != "[a h[1-2] h1]" {
			t.Errorf("definition: %v", d.Targets)
		}
		f.probes("a", "", 2)
		f.probes("b", "timeout", 4)
		synctest.Wait()

		// Stopping
		s, err := e.Stop("w")
		if err != nil || s.State != api.StateStopping || s.Reason != api.ReasonRequested {
			t.Errorf("stop: %+v, %v", s, err)
		}
		synctest.Wait()
		_, err = e.Extend("w", 3600*3)
		wantCode(t, err, api.CodeConflict)
		wantCode(t, e.Delete("w", false), api.CodeConflict)
		_, err = e.Create(context.Background(), request("w"))
		wantCode(t, err, api.CodeConflict)
		e.wantKinds("w", api.EventWatchStarted, "outage_start")
		f.probe("a", "") // the last replies still count
		time.Sleep(time.Second)
		close(f.gate)
		synctest.Wait()

		// Stopped
		e.wantState("w", api.StateStopped, api.ReasonRequested)
		e.wantKinds("w", api.EventWatchStarted, "outage_start", "outage_end", api.EventWatchStopped)
		ev := e.events("w")
		if o := ev[1].Outage; o == nil || !o.Ongoing || o.EndUnknown || o.DurationMs != 2000 {
			t.Errorf("outage_start: %+v", o)
		}
		if o := ev[2].Outage; o == nil || !o.EndUnknown || o.End != nil || o.DurationMs != 4000 || o.LostProbes != 4 || ev[2].Target != "b" {
			t.Errorf("outage_end: %+v", o)
		}
		if r := ev[3].Stopped; r == nil || r.Reason != api.ReasonRequested {
			t.Errorf("watch_stopped: %+v", r)
		}
		first := e.summary("w", true)
		if first.Counts != (api.Counts{Down: 1, OK: 1}) || first.Targets[1].Success != 3 || !first.Targets[0].Outages[0].EndUnknown || first.StoppedAt == nil || !first.AsOf.Equal(*first.StoppedAt) {
			t.Errorf("final summary: %+v", first)
		}
		if first.Targets[0].TotalDowntimeMs != 4000 {
			t.Errorf("downtime %v", first.Targets[0].TotalDowntimeMs)
		}
		time.Sleep(time.Hour)
		a, _ := json.Marshal(first)
		if b, _ := json.Marshal(e.summary("w", true)); string(a) != string(b) {
			t.Errorf("the final summary changed:\n%s\n%s", a, b)
		}
		if w := e.watches[0]; w.sess != nil {
			t.Error("the session is kept")
		}
		f.params.Stats.OnEvent(stats.Event{Kind: stats.EventOutageEnd}) // too late
		e.wantKinds("w", api.EventWatchStarted, "outage_start", "outage_end", api.EventWatchStopped)
		if s, err := e.Stop("w"); err != nil || s.State != api.StateStopped {
			t.Errorf("stop of a stopped watch: %+v, %v", s, err)
		}
	})
}

func TestRequests(t *testing.T) {
	long := strings.Repeat("x", 64)
	for _, tc := range []struct {
		name string
		req  api.CreateRequest
		ok   bool
	}{
		{"default", api.CreateRequest{}, true},
		{"longest name", api.CreateRequest{Name: long[1:]}, true},
		{"too long name", api.CreateRequest{Name: long}, false},
		{"bad name", api.CreateRequest{Name: "a/b"}, false},
		{"bad first", api.CreateRequest{Name: ".a"}, false},
		{"line break", api.CreateRequest{Name: "a\n"}, false},
		{"like an ID", api.CreateRequest{Name: "w-0123456789ab"}, false},
		{"not like an ID", api.CreateRequest{Name: "w-switch-1"}, true},
		{"shortest interval", api.CreateRequest{IntervalMs: 200}, true},
		{"too short interval", api.CreateRequest{IntervalMs: 199}, false},
		{"negative interval", api.CreateRequest{IntervalMs: -1000}, false},
		{"overflowing interval", api.CreateRequest{IntervalMs: 1 << 62}, false},
		{"longest timeout", api.CreateRequest{TimeoutMs: 60000}, true},
		{"too long timeout", api.CreateRequest{TimeoutMs: 60001}, false},
		{"negative timeout", api.CreateRequest{TimeoutMs: -1}, false},
		{"longest duration", api.CreateRequest{DurationS: 7 * 24 * 3600}, true},
		{"too long duration", api.CreateRequest{DurationS: 7*24*3600 + 1}, false},
		{"negative duration", api.CreateRequest{DurationS: -1}, false},
		{"overflowing duration", api.CreateRequest{DurationS: 1 << 62}, false},
		{"no targets", api.CreateRequest{Targets: []string{}}, false},
		{"empty target", api.CreateRequest{Targets: []string{"a", ""}}, false},
		{"most targets", api.CreateRequest{Targets: []string{"h[1-1024]"}}, true},
		{"too many targets", api.CreateRequest{Targets: []string{"h[1-1025]"}}, false},
		{"far too many targets", api.CreateRequest{Targets: []string{"10.0.0.0/8"}}, false},
		{"too many specs", api.CreateRequest{Targets: make([]string, 1025)}, false},
		{"long target", api.CreateRequest{Targets: []string{strings.Repeat("0", 3000) + "[1-1000]"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bubble(t, DefaultLimits, func(t *testing.T, e *env) {
				if tc.req.Name == "" {
					tc.req.Name = "w"
				}
				if tc.req.Targets == nil {
					tc.req.Targets = []string{"a"}
				}
				_, err := e.Create(context.Background(), tc.req)
				if tc.ok && err != nil {
					t.Error(err)
				}
				if !tc.ok {
					wantCode(t, err, api.CodeInvalid)
				}
			})
		})
	}
}

func TestNames(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		var ids []string
		for range 3 {
			w, _ := e.start(request("w"))
			_, err := e.Create(context.Background(), request("w"))
			wantCode(t, err, api.CodeConflict)
			if got, _ := e.Get("w"); got.ID != w.ID {
				t.Errorf("the name is of %s, not the latest %s", got.ID, w.ID)
			}
			e.Stop("w")
			synctest.Wait()
			ids = append(ids, w.ID)
		}
		other, _ := e.start(request("w2"))
		list := e.List()
		if len(list) != 4 || list[3].ID != other.ID {
			t.Errorf("list: %+v", list)
		}
		for i, id := range ids {
			if got, err := e.Get(id); err != nil || got.ID != id || list[i].ID != id || !idFormat.MatchString(id) {
				t.Errorf("%s: %+v, %v", id, got, err)
			}
		}
		if err := e.Delete(ids[2], false); err != nil {
			t.Error(err)
		}
		if got, _ := e.Get("w"); got.ID != ids[1] {
			t.Errorf("the name is of %s", got.ID)
		}
		for _, id := range []string{"", "nope", "W", "a\n\x1b[31m", ids[2]} {
			_, err := e.Get(id)
			wantCode(t, err, api.CodeNotFound)
		}
	})
}

func TestDelete(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.held = true
		_, f := e.start(request("w"))
		wantCode(t, e.Delete("w", false), api.CodeConflict)
		if err := e.Delete("w", true); err != nil {
			t.Fatal(err)
		}
		e.wantState("w", api.StateStopping, api.ReasonRequested) // still uses resources
		close(f.gate)
		synctest.Wait()
		_, err := e.Get("w")
		wantCode(t, err, api.CodeNotFound)
	})
}

// A watch takes its name and slot while it is created, and gives them back if
// that fails.
func TestReservation(t *testing.T) {
	limits := DefaultLimits
	limits.Running = 3
	bubble(t, limits, func(t *testing.T, e *env) {
		proceed := make(chan error)
		e.open = func(context.Context, *fakeSession) error { return <-proceed }
		results := make(chan error, 3)
		for i := range 3 {
			go func() {
				_, err := e.Create(context.Background(), request(fmt.Sprint("w", i)))
				results <- err
			}()
		}
		synctest.Wait()
		if len(e.List()) != 0 {
			t.Error("a watch is listed before it is there")
		}
		_, err := e.Get("w0")
		wantCode(t, err, api.CodeNotFound)
		_, err = e.Create(context.Background(), request("w0"))
		wantCode(t, err, api.CodeConflict)
		_, err = e.Create(context.Background(), request("w3"))
		wantCode(t, err, api.CodeLimit)
		wantCode(t, e.Close(), api.CodeConflict)
		if e.CloseIdle(0) {
			t.Error("idle while a watch is created")
		}
		proceed <- errors.New("no")
		wantCode(t, <-results, api.CodeInternal)
		proceed <- nil
		proceed <- nil
		if err := errors.Join(<-results, <-results); err != nil {
			t.Error(err)
		}
		e.open = nil
		e.start(request("w4"))
		_, err = e.Create(context.Background(), request("w5"))
		wantCode(t, err, api.CodeLimit)
		e.Stop("w4") // a stopped one counts no more
		synctest.Wait()
		e.start(request("w5"))
	})
}

func TestCreateErrors(t *testing.T) {
	two := prober.TargetErrors{{Target: "a\x1b", Err: errors.New("no\nsuch host")}, {Target: "b", Err: errors.New("x")}}
	one := prober.ProberError{Prober: "icmpv4", Err: errors.New("not permitted")}
	for _, tc := range []struct {
		err     error
		code    string
		message string
		targets int
	}{
		{fmt.Errorf("add: %w", two), api.CodeUnresolvable, "2 of 3 targets", 2},
		{one, api.CodeUnresolvable, "not permitted", 0},
		{errors.Join(one, two), api.CodeUnresolvable, "not permitted", 2},
		{errors.New("other"), api.CodeInternal, "other", 0},
	} {
		bubble(t, DefaultLimits, func(t *testing.T, e *env) {
			e.open = func(context.Context, *fakeSession) error { return tc.err }
			_, err := e.Create(context.Background(), request("w", "a\x1b", "b", "c"))
			wantCode(t, err, tc.code)
			ae := err.(*api.Error)
			if !strings.Contains(ae.Message, tc.message) || len(ae.Targets) != tc.targets {
				t.Errorf("got %+v", ae)
			}
			if tc.targets > 0 && (ae.Targets[0] != api.TargetError{Target: "a", Message: "no such host"}) {
				t.Errorf("got %+v", ae.Targets)
			}
		})
	}
}

func TestCreateEnds(t *testing.T) {
	hang := func(ctx context.Context, _ *fakeSession) error {
		<-ctx.Done()
		return ctx.Err()
	}
	t.Run("timeout", func(t *testing.T) {
		bubble(t, DefaultLimits, func(t *testing.T, e *env) {
			e.open = hang
			start := time.Now()
			_, err := e.Create(context.Background(), request("w"))
			wantCode(t, err, api.CodeUnresolvable)
			if d := time.Since(start); d != 30*time.Second {
				t.Errorf("took %v", d)
			}
		})
	})
	t.Run("canceled", func(t *testing.T) {
		bubble(t, DefaultLimits, func(t *testing.T, e *env) {
			e.open = hang
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(time.Second, cancel)
			start := time.Now()
			if _, err := e.Create(ctx, request("w")); err == nil || time.Since(start) != time.Second {
				t.Errorf("after %v: %v", time.Since(start), err)
			}
		})
	})
	for _, late := range []bool{false, true} { // the session is there in spite of ctx
		t.Run(fmt.Sprint("shutdown, late ", late), func(t *testing.T) {
			bubble(t, DefaultLimits, func(t *testing.T, e *env) {
				e.open = func(ctx context.Context, f *fakeSession) error {
					err := hang(ctx, f)
					if late {
						time.Sleep(time.Second)
						return nil
					}
					return err
				}
				result := make(chan error)
				go func() {
					_, err := e.Create(context.Background(), request("w"))
					result <- err
				}()
				synctest.Wait()
				go e.Shutdown(context.Background())
				wantCode(t, <-result, api.CodeShuttingDown)
				if late {
					<-e.sessions[0].settled // or the bubble deadlocks
				}
				if len(e.List()) != 0 {
					t.Error("the watch is there")
				}
			})
		})
	}
}

func TestSessionFails(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{errors.New("prober\x1b icmpv4: gone"), "prober icmpv4: gone"}, {nil, "probing ended"}} {
		bubble(t, DefaultLimits, func(t *testing.T, e *env) {
			e.held = true
			_, f := e.start(request("w"))
			f.fail <- tc.err
			synctest.Wait()
			if s, _ := e.state("w"); s == api.StateStopped {
				t.Error("stopped before the session has ended")
			}
			close(f.gate)
			synctest.Wait()
			e.wantState("w", api.StateStopped, api.ReasonFailed)
			w, _ := e.Get("w")
			ev := e.events("w")
			if s := ev[len(ev)-1].Stopped; s == nil || s.Error != tc.want || w.Error != tc.want {
				t.Errorf("event %+v, watch %+v", s, w)
			}
		})
	}
}

func TestExpiry(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		req := request("w")
		req.DurationS = 3601
		w, _ := e.start(req)
		time.Sleep(time.Hour + time.Second - time.Nanosecond)
		e.wantState("w", api.StateRunning, "")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonExpired)
		if d, _ := e.Get("w"); !d.StoppedAt.Equal(w.ExpiresAt) {
			t.Errorf("stopped at %v, not %v", d.StoppedAt, w.ExpiresAt)
		}
	})
}

func TestExtend(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		w, _ := e.start(request("w"))
		time.Sleep(time.Hour)
		for _, s := range []int64{0, -1, 7*24*3600 + 1} {
			_, err := e.Extend("w", s)
			wantCode(t, err, api.CodeInvalid)
		}
		_, err := e.Extend("w", 3599) // would shorten it
		wantCode(t, err, api.CodeConflict)
		x, err := e.Extend("w", 3*3600)
		if err != nil || x.ExpiresAt.Sub(w.StartedAt) != 4*time.Hour {
			t.Errorf("%+v, %v", x, err)
		}
		e.wantKinds("w", api.EventWatchStarted, api.EventWatchExtended)
		if ev := e.events("w")[1].Extended; !ev.OldExpiresAt.Equal(w.ExpiresAt) || !ev.NewExpiresAt.Equal(x.ExpiresAt) {
			t.Errorf("%+v", ev)
		}
		time.Sleep(3*time.Hour - time.Nanosecond)
		e.wantState("w", api.StateRunning, "")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonExpired)
	})
}

// A suspended host: its wall clock has advanced, the monotonic one has not.
func TestGap(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.start(request("stopped"))
		e.Stop("stopped")
		time.Sleep(20 * time.Second)
		w, _ := e.start(request("w")) // at a tick
		before := w.StartedAt
		e.setJump(4 * time.Second) // below the threshold
		time.Sleep(10 * time.Second)
		synctest.Wait()
		e.wantKinds("w", api.EventWatchStarted)

		e.setJump(time.Hour)
		e.start(request("late")) // after it
		time.Sleep(2 * time.Second)
		synctest.Wait()
		e.wantKinds("w", api.EventWatchStarted, api.EventGap)
		e.wantKinds("stopped", api.EventWatchStarted, api.EventWatchStopped)
		g := e.events("w")[1].Gap
		if g == nil || !g.Start.Equal(before.Add(14*time.Second)) || !g.End.Equal(before.Add(time.Hour+12*time.Second)) ||
			g.DurationMs != g.End.Sub(g.Start).Milliseconds() {
			t.Errorf("gap %+v after %v", g, before)
		}
		e.wantKinds("late", api.EventWatchStarted)
		if s := e.summary("w", false); s.GapCount != 1 || s.LastGap == nil || *s.LastGap != *g {
			t.Errorf("summary %+v", s)
		}

		e.setJump(0) // back
		time.Sleep(10 * time.Second)
		synctest.Wait()
		e.wantKinds("w", api.EventWatchStarted, api.EventGap)

		// Beyond the end, which is noticed at the next tick, after the gap
		e.setJump(3 * time.Hour)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonExpired)
		e.wantKinds("w", api.EventWatchStarted, api.EventGap, api.EventGap, api.EventWatchStopped)
	})
}

// A wall clock that is set back does not lengthen a watch, and an expired watch
// cannot be extended, although no tick has noticed yet.
func TestEndByEitherClock(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.start(request("w"))
		e.setJump(-24 * time.Hour)
		time.Sleep(2*time.Hour - time.Second)
		e.wantState("w", api.StateRunning, "")
		e.wantKinds("w", api.EventWatchStarted)
		time.Sleep(time.Second)
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonExpired)

		time.Sleep(time.Second)
		e.start(request("x"))
		e.setJump(0)
		_, err := e.Extend("x", 24*3600)
		wantCode(t, err, api.CodeConflict)
		synctest.Wait()
		e.wantState("x", api.StateStopped, api.ReasonExpired)
	})
}

func TestRetention(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.start(request("w"))
		req := request("running")
		req.DurationS = 3 * 24 * 3600
		e.start(req)
		e.Stop("w")
		time.Sleep(24*time.Hour - time.Second)
		e.wantState("w", api.StateStopped, api.ReasonRequested)
		time.Sleep(3 * time.Second)
		_, err := e.Get("w")
		wantCode(t, err, api.CodeNotFound)
		e.wantState("running", api.StateRunning, "")
	})
}

func TestClose(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		time.Sleep(time.Minute)
		if e.CloseIdle(time.Minute + 1) {
			t.Error("closed too early")
		}
		e.start(request("w"))
		wantCode(t, e.Close(), api.CodeConflict)
		e.Stop("w")
		synctest.Wait()
		time.Sleep(time.Hour)
		if e.CloseIdle(time.Minute) {
			t.Error("closed with a stopped watch")
		}
		e.Delete("w", false)
		time.Sleep(time.Minute - 1)
		if e.CloseIdle(time.Minute) {
			t.Error("closed too early after the last watch")
		}
		time.Sleep(time.Nanosecond)
		if !e.CloseIdle(time.Minute) {
			t.Error("not closed")
		}
		_, err := e.Create(context.Background(), request("w"))
		wantCode(t, err, api.CodeShuttingDown)
	})
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.start(request("w"))
		e.Stop("w")
		synctest.Wait()
		if err := e.Close(); err != nil { // with a stopped watch
			t.Error(err)
		}
		_, err := e.Create(context.Background(), request("w"))
		wantCode(t, err, api.CodeShuttingDown)
	})
}

func TestShutdown(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		e.held = true
		_, f := e.start(request("w"))
		e.start(request("x"))
		e.Stop("x")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v", err)
		}
		e.wantState("w", api.StateStopping, api.ReasonShutdown)
		close(f.gate)
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonShutdown)
		e.wantState("x", api.StateStopping, api.ReasonRequested)
	})
}

// The config is loaded for each watch.
func TestConfig(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		cfg, loadErr := config.DefaultConfig(), error(nil)
		e.config = func() (*config.Config, error) { return cfg, loadErr }
		_, f := e.start(request("w"))
		if f.params.Config != cfg {
			t.Error("another config")
		}
		cfg = config.DefaultConfig()
		if _, f = e.start(request("x")); f.params.Config != cfg {
			t.Error("the config is not loaded again")
		}
		loadErr = errors.New("bad yaml")
		_, err := e.Create(context.Background(), request("y"))
		wantCode(t, err, api.CodeInternal)
		if !strings.Contains(err.Error(), "bad yaml") {
			t.Error(err)
		}
	})
}

func TestStatuses(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		_, f := e.start(request("w", "h[1-9]"))
		f.results <- &prober.Event{Key: "pending", DisplayName: "pending", Result: prober.REGISTER}
		f.probe("never\x1b", "refused\n\u202e")
		f.probes("ok", "", 2)
		f.probes("recovered", "lost", 3)
		f.probes("recovered", "", 1)
		f.probes("lost long ago", "lost", 1)
		f.probes("lost long ago", "", 5)
		f.probes("failing", "", 1)
		f.probes("failing", "lost", 2)
		f.probes("down", "", 1)
		f.probes("down", "lost\x1b", 150)
		for range 7 {
			f.probes("flapping", "lost", 3)
			f.probes("flapping", "", 1)
		}
		synctest.Wait()
		s := e.summary("w", true)
		if s.Counts != (api.Counts{Down: 2, Degraded: 4, Pending: 1, OK: 1}) {
			t.Errorf("counts %+v", s.Counts)
		}
		var got []string
		byHost := make(map[string]api.TargetSummary)
		for _, ts := range s.Targets {
			got = append(got, fmt.Sprintf("%s %s %d", ts.Host, ts.Status, ts.ConsecutiveFailures))
			byHost[ts.Host] = ts
		}
		want := []string{
			"down down 150", "never down 1",
			"failing degraded 2", "flapping degraded 0", "lost long ago degraded 0", "recovered degraded 0",
			"pending pending 0", "ok ok 0",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("targets:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if o := byHost["down"].Outages[0]; !o.Ongoing || o.EndUnknown || o.LastError != "lost" {
			t.Errorf("down: %+v", o)
		}
		if o := byHost["flapping"]; o.OutageCount != 7 || len(o.Outages) != 5 || !o.Outages[4].End.After(*o.Outages[0].End) {
			t.Errorf("flapping: %+v", o)
		}
		if r := byHost["never"].LastFailReason; r != "refused" {
			t.Errorf("reason %q", r)
		}
		if s := e.summary("w", false); len(s.Targets) != 7 || s.OmittedTargets != 0 {
			t.Errorf("%d targets, %d omitted", len(s.Targets), s.OmittedTargets)
		}
	})
}

func TestSummaryLimit(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		_, f := e.start(request("w", "h[1-130]"))
		for i := range 130 {
			errMsg := "lost"
			if i%13 == 0 {
				errMsg = ""
			}
			f.probe(fmt.Sprint("h", i), errMsg)
		}
		synctest.Wait()
		if s := e.summary("w", false); len(s.Targets) != 100 || s.OmittedTargets != 20 || s.Counts.OK != 10 {
			t.Errorf("%d targets, %d omitted, %+v", len(s.Targets), s.OmittedTargets, s.Counts)
		}
		if s := e.summary("w", true); len(s.Targets) != 130 || s.OmittedTargets != 0 {
			t.Errorf("%d targets, %d omitted", len(s.Targets), s.OmittedTargets)
		}
	})
}

func TestEventLog(t *testing.T) {
	l := &eventLog{size: 5}
	for range 12 {
		l.add(api.Event{})
	}
	for _, tc := range []struct {
		after     int64
		limit     int
		want      string
		next      int64
		more, cut bool
	}{
		{0, 99, "[8 9 10 11 12]", 12, false, true},
		{0, 2, "[8 9]", 9, true, true},
		{6, 99, "[8 9 10 11 12]", 12, false, true},
		{7, 99, "[8 9 10 11 12]", 12, false, false},
		{9, 1, "[10]", 10, true, false},
		{11, 1, "[12]", 12, false, false},
		{12, 99, "[]", 12, false, false},
		{13, 99, "[]", 13, false, false},
		{1 << 62, 99, "[]", 1 << 62, false, false},
	} {
		got := l.read(tc.after, tc.limit)
		seqs := []int64{}
		for _, e := range got.Events {
			seqs = append(seqs, e.Seq)
		}
		if fmt.Sprint(seqs) != tc.want || got.Next != tc.next || got.More != tc.more || got.Truncated != tc.cut {
			t.Errorf("read(%d, %d) = %v next %d more %v truncated %v", tc.after, tc.limit, seqs, got.Next, got.More, got.Truncated)
		}
	}
	if last, dropped := l.seqs(); last != 12 || dropped != 7 {
		t.Errorf("last %d, dropped %d", last, dropped)
	}
}

// What others can write is not in an event, or cleaned.
func TestEventsOfStats(t *testing.T) {
	l := &eventLog{size: 99}
	now := time.Now()
	ev := func(e stats.Event) {
		e.Name, e.Time, e.RecordedAt = "h\x1b[31m", now, now.Add(time.Second)
		l.addStats(e)
	}
	ev(stats.Event{Kind: stats.EventTTLChange, TTL: &stats.TTLChange{Old: 60, New: 58}})
	ev(stats.Event{Kind: stats.EventTTLChange, TTL: &stats.TTLChange{Old: 60, New: 250}})
	ev(stats.Event{Kind: stats.EventPeerChange, Peer: &stats.PeerChange{Old: "192.0.2.1:443", New: "[2001:db8::1]:443"}})
	ev(stats.Event{Kind: stats.EventPeerChange, Peer: &stats.PeerChange{Old: "evil", New: "example.com:80"}})
	ev(stats.Event{Kind: stats.EventCertChange, Cert: &stats.CertChange{
		Old: prober.TLSDetails{SubjectCN: "evil", Issuer: "evil", NotAfter: now},
		New: prober.TLSDetails{SubjectCN: "evil", Issuer: "evil", NotAfter: now.Add(time.Hour)},
	}})
	ev(stats.Event{Kind: stats.EventOutageEnd, Outage: &stats.Outage{Start: now, End: now.Add(time.Second), LostProbes: 3, LastError: "no\x00pe"}})
	var b strings.Builder
	for _, e := range l.read(0, 99).Events {
		if e.Target != "h[31m" || !e.Time.Equal(now) || !e.RecordedAt.Equal(now.Add(time.Second)) {
			t.Errorf("%+v", e)
		}
		e.Time, e.RecordedAt = time.Time{}, time.Time{}
		j, _ := json.Marshal([]any{e.TTL, e.Peer, e.Cert, e.Outage})
		fmt.Fprintf(&b, "%s %s\n", e.Kind, j)
	}
	format := func(t time.Time) string { j, _ := json.Marshal(t); return string(j) }
	want := `ttl_change [{"old":60,"new":58,"hops_delta":2},null,null,null]
ttl_change [{"old":60,"new":250},null,null,null]
peer_change [null,{"old":"192.0.2.1:443","new":"[2001:db8::1]:443"},null,null]
peer_change [null,{"old":"","new":""},null,null]
cert_change [null,null,{"old_not_after":` + format(now) + `,"new_not_after":` + format(now.Add(time.Hour)) + `},null]
outage_end [null,null,null,{"start":` + format(now) + `,"end":` + format(now.Add(time.Second)) + `,"duration_ms":1000,"lost_probes":3,"ongoing":false,"last_error":"nope"}]
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// Events of a running watch are there while it is stopped, read and extended.
func TestHookDoesNotDeadlock(t *testing.T) {
	bubble(t, DefaultLimits, func(t *testing.T, e *env) {
		_, f := e.start(request("w"))
		go func() {
			for i := range 1000 {
				f.probe("a", []string{"lost", "lost", "lost", ""}[i%4])
				time.Sleep(time.Millisecond)
			}
			f.fail <- nil
		}()
		for range 100 {
			e.Summary("w", true)
			e.Events("w", 0, 10)
			e.Extend("w", 7200)
			e.List()
			time.Sleep(10 * time.Millisecond)
		}
		synctest.Wait()
		e.wantState("w", api.StateStopped, api.ReasonFailed)
		ends := 0
		for _, ev := range e.events("w") {
			if ev.Kind == "outage_end" {
				ends++
			}
		}
		if ends != 250 {
			t.Errorf("%d outages", ends)
		}
	})
}
