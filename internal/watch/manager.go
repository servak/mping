// Package watch runs the watches of the server: sessions with a name and an end
// whose results stay readable afterwards.
//
// The lock of the Manager comes before that of an eventLog. Neither is held
// while a session is called, whose hook takes the latter.
package watch

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/session"
	"github.com/servak/mping/internal/stats"
)

const (
	createTimeout = 30 * time.Second // of the lookups of target names
	retention     = 24 * time.Hour   // of stopped watches
	tickInterval  = 2 * time.Second
	gapThreshold  = 5 * time.Second // by which the wall clock may advance more than the monotonic one
)

// Session is the part of *session.Session that a watch needs.
type Session interface {
	Start(ctx context.Context) <-chan error
	Stop()
	Metrics() stats.MetricsManager
}

type newSession func(ctx context.Context, p session.Params) (Session, error)

// instant is a moment on both clocks: a suspended host stops the monotonic one.
type instant struct {
	wall time.Time // without monotonic reading
	mono time.Duration
}

func clock() func() instant {
	start := time.Now()
	return func() instant {
		now := time.Now()
		return instant{wall: now.Round(0), mono: now.Sub(start)}
	}
}

// Manager holds the watches. Shutdown must be called to release it.
type Manager struct {
	loadConfig func() (*config.Config, error)
	newSession newSession
	limits     Limits
	now        func() instant

	ctx    context.Context // done once Shutdown has begun
	cancel context.CancelFunc
	wg     sync.WaitGroup // sessions and Creates; added to only while not closed

	mu         sync.Mutex
	closed     bool // no new watches
	watches    []*watch
	emptySince time.Duration // monotonic; while there are no watches
}

// watch is a watch, or the reservation of one that is being created. The fields
// after log are guarded by the lock of the manager.
type watch struct {
	id, name string
	source   string
	specs    []string
	targets  int
	interval time.Duration
	timeout  time.Duration
	duration time.Duration
	ctx      context.Context // of the session
	cancel   context.CancelFunc
	log      *eventLog

	state   string // "" while it is being created: only reserve sees it then
	reason  string
	err     string
	started time.Time
	expires time.Time     // wall clock
	expMono time.Duration // the same end on the monotonic clock, should the wall clock be set back
	timer   *time.Timer   // fires at expMono
	stopped time.Time
	removed bool    // remove it once it has stopped
	checked instant // for gaps
	gaps    int
	lastGap *api.Gap
	sess    Session   // while running or stopping
	final   *snapshot // once stopped
}

// NewManager returns a Manager that loads the config, which must be valid, for
// each watch anew.
func NewManager(loadConfig func() (*config.Config, error)) *Manager {
	return newManager(loadConfig, func(ctx context.Context, p session.Params) (Session, error) {
		s, err := session.New(ctx, p)
		if err != nil {
			return nil, err // not a nil *session.Session
		}
		return s, nil
	}, DefaultLimits, clock())
}

func newManager(load func() (*config.Config, error), open newSession, limits Limits, now func() instant) *Manager {
	m := &Manager{loadConfig: load, newSession: open, limits: limits, now: now, emptySince: now().mono}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Go(func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.tick()
			}
		}
	})
	return m
}

// tick looks for gaps, ends the watches that have expired and removes old ones.
func (m *Manager) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, w := range slices.Clone(m.watches) {
		if w.state == api.StateRunning {
			w.check(now)
		}
		m.expire(w, now)
		if w.state == api.StateStopped && now.wall.Sub(w.stopped) >= retention {
			m.remove(w)
		}
	}
}

// check records a gap since the last check, if there is one.
func (w *watch) check(now instant) {
	last := w.checked
	w.checked = now
	d := now.wall.Sub(last.wall)
	if d-(now.mono-last.mono) <= gapThreshold {
		return
	}
	g := api.Gap{Start: last.wall, End: now.wall, DurationMs: d.Milliseconds()}
	w.gaps++
	w.lastGap = &g
	w.log.add(api.Event{Kind: api.EventGap, Time: g.Start, RecordedAt: g.End, Gap: &g})
}

// expire stops w if it is running and its end has come.
func (m *Manager) expire(w *watch, now instant) {
	if w.state == api.StateRunning && (!now.wall.Before(w.expires) || now.mono >= w.expMono) {
		w.stop(api.ReasonExpired)
	}
}

// setEnd makes w end in d.
func (m *Manager) setEnd(w *watch, now instant, d time.Duration) {
	if w.timer != nil {
		w.timer.Stop()
	}
	w.expires, w.expMono = now.wall.Add(d), now.mono+d
	w.timer = time.AfterFunc(d, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.expire(w, m.now())
	})
}

// stop begins to stop a running watch; run does the rest.
func (w *watch) stop(reason string) {
	w.state, w.reason = api.StateStopping, reason
	w.timer.Stop()
	w.cancel()
}

func (m *Manager) remove(w *watch) {
	m.watches = slices.DeleteFunc(m.watches, func(o *watch) bool { return o == w })
	if len(m.watches) == 0 {
		m.emptySince = m.now().mono
	}
}

// run runs the session of a watch to its end.
func (m *Manager) run(w *watch, sess Session) {
	defer m.wg.Done()
	var err error
	for e := range sess.Start(w.ctx) { // closed when all has settled
		err = e
	}
	m.mu.Lock()
	if w.state == api.StateRunning { // nobody asked for it
		if err == nil {
			err = errors.New("probing ended")
		}
		w.err = api.Clean(err.Error(), maxText)
		w.stop(api.ReasonFailed)
	}
	m.mu.Unlock()

	final := takeSnapshot(sess, true)

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range final.outageEnds() {
		w.log.add(e)
	}
	w.stopped = m.now().wall
	w.log.add(api.Event{
		Kind: api.EventWatchStopped, Time: w.stopped, RecordedAt: w.stopped,
		Stopped: &api.Stopped{Reason: w.reason, Error: w.err},
	})
	w.state, w.sess, w.final = api.StateStopped, nil, final
	if w.removed {
		m.remove(w)
	}
}

// get returns the watch with an ID or, failing that, the latest with a name.
// That is the running one, if any, since its name cannot be taken.
func (m *Manager) get(idOrName string) (*watch, error) {
	var found *watch
	for _, w := range m.watches {
		if w.state != "" && (w.id == idOrName || w.name == idOrName) { // no name looks like an ID
			found = w
		}
	}
	if found == nil {
		return nil, api.Errorf(api.CodeNotFound, "no watch %q", idOrName)
	}
	return found, nil
}

// Extend makes a running watch end in d. It does not shorten it.
func (m *Manager) Extend(idOrName string, seconds int64) (api.Watch, error) {
	d, err := m.limits.duration(seconds)
	if err != nil {
		return api.Watch{}, err
	}
	return m.change(idOrName, func(w *watch) error {
		now := m.now()
		m.expire(w, now)
		if w.state != api.StateRunning {
			return api.Errorf(api.CodeConflict, "watch %s is %s", w.name, w.state)
		}
		old := w.expires
		if now.wall.Add(d).Before(old) {
			return api.Errorf(api.CodeConflict, "watch %s would end earlier than now (%s)", w.name, old.Format(time.RFC3339))
		}
		m.setEnd(w, now, d)
		w.log.add(api.Event{
			Kind: api.EventWatchExtended, Time: now.wall, RecordedAt: now.wall,
			Extended: &api.Extended{OldExpiresAt: old, NewExpiresAt: w.expires},
		})
		return nil
	})
}

// Stop begins to stop a watch. It does nothing to one that is not running.
func (m *Manager) Stop(idOrName string) (api.Watch, error) {
	return m.change(idOrName, func(w *watch) error {
		if w.state == api.StateRunning {
			w.stop(api.ReasonRequested)
		}
		return nil
	})
}

// change applies f to a watch and returns the outcome.
func (m *Manager) change(idOrName string, f func(*watch) error) (api.Watch, error) {
	m.mu.Lock()
	w, err := m.get(idOrName)
	if err == nil {
		err = f(w)
	}
	if err != nil {
		m.mu.Unlock()
		return api.Watch{}, err
	}
	v := w.view()
	m.mu.Unlock()
	v.snapshot()
	return v.Watch, nil
}

// Delete removes a watch, with force also one that has not stopped, which then
// goes away when it has.
func (m *Manager) Delete(idOrName string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.get(idOrName)
	switch {
	case err != nil:
		return err
	case w.state == api.StateStopped:
		m.remove(w)
		return nil
	case !force:
		return api.Errorf(api.CodeConflict, "watch %s is %s", w.name, w.state)
	}
	w.removed = true
	if w.state == api.StateRunning {
		w.stop(api.ReasonRequested)
	}
	return nil
}

// Close takes no new watches from now on, unless some have not stopped, which
// is an error.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := m.active(); n > 0 {
		return api.Errorf(api.CodeConflict, "watches still running: %d", n)
	}
	m.closed = true
	return nil
}

// CloseIdle takes no new watches from now on if there have been none, not even
// stopped ones, for d, and tells whether that is so.
func (m *Manager) CloseIdle(d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.watches) > 0 || m.now().mono-m.emptySince < d {
		return false
	}
	m.closed = true
	return true
}

// active returns how many watches have not stopped, including those that are
// being created.
func (m *Manager) active() (n int) {
	for _, w := range m.watches {
		if w.state != api.StateStopped {
			n++
		}
	}
	return n
}

// Shutdown stops all watches and waits until they have stopped or ctx is done.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	for _, w := range m.watches {
		if w.state == api.StateRunning {
			w.stop(api.ReasonShutdown)
		}
	}
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
