package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/session"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/target"
)

// Limits bound what watches may use.
type Limits struct {
	Running         int // watches that have not stopped
	Targets         int // of a watch
	MinInterval     time.Duration
	MaxTimeout      time.Duration
	DefaultDuration time.Duration
	MaxDuration     time.Duration
	Events          int // kept of a watch
}

// DefaultLimits are those of NewManager.
var DefaultLimits = Limits{
	Running:         20,
	Targets:         1024,
	MinInterval:     200 * time.Millisecond,
	MaxTimeout:      time.Minute,
	DefaultDuration: 2 * time.Hour,
	MaxDuration:     7 * 24 * time.Hour,
	Events:          10000,
}

const (
	defaultInterval = 1000 // ms
	defaultTimeout  = 1000 // ms

	// maxSpec is the length of a target specification, which its expansion
	// multiplies.
	maxSpec = 2048
)

var (
	nameFormat = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	idFormat   = regexp.MustCompile(`^w-[0-9a-f]{12}$`)
)

func newID() string {
	b := make([]byte, 6)
	rand.Read(b) // never fails
	return "w-" + hex.EncodeToString(b)
}

// duration checks the duration of a watch from now on.
func (l *Limits) duration(seconds int64) (time.Duration, error) {
	if seconds <= 0 || seconds > int64(l.MaxDuration/time.Second) {
		return 0, api.Errorf(api.CodeInvalid, "duration must be between 1s and %v, got %ds", l.MaxDuration, seconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// prepare checks a request and returns the watch it asks for, not registered
// yet, and its expanded targets.
func (m *Manager) prepare(req api.CreateRequest) (*watch, []string, error) {
	if !nameFormat.MatchString(req.Name) || idFormat.MatchString(req.Name) {
		return nil, nil, api.Errorf(api.CodeInvalid, "invalid name %q: use up to 63 letters, digits, '.', '_' and '-', not like an ID, the first a letter or digit", req.Name)
	}
	if req.DurationS == 0 {
		req.DurationS = int64(m.limits.DefaultDuration / time.Second)
	}
	if req.IntervalMs == 0 {
		req.IntervalMs = defaultInterval
	}
	if req.TimeoutMs == 0 {
		req.TimeoutMs = defaultTimeout
	}
	duration, err := m.limits.duration(req.DurationS)
	if err != nil {
		return nil, nil, err
	}
	if req.IntervalMs < m.limits.MinInterval.Milliseconds() || req.IntervalMs > m.limits.MaxDuration.Milliseconds() {
		return nil, nil, api.Errorf(api.CodeInvalid, "interval must be between %v and %v, got %dms", m.limits.MinInterval, m.limits.MaxDuration, req.IntervalMs)
	}
	if req.TimeoutMs <= 0 || req.TimeoutMs > m.limits.MaxTimeout.Milliseconds() {
		return nil, nil, api.Errorf(api.CodeInvalid, "timeout must be between 1ms and %v, got %dms", m.limits.MaxTimeout, req.TimeoutMs)
	}
	targets, err := m.expand(req.Targets)
	if err != nil {
		return nil, nil, err
	}
	w := &watch{
		name:     req.Name,
		source:   req.SourceInterface,
		targets:  len(targets),
		interval: time.Duration(req.IntervalMs) * time.Millisecond,
		timeout:  time.Duration(req.TimeoutMs) * time.Millisecond,
		duration: duration,
		log:      &eventLog{size: m.limits.Events},
	}
	for _, s := range req.Targets {
		w.specs = append(w.specs, api.Clean(s, maxSpec))
	}
	return w, targets, nil
}

// expand returns the targets that specs stand for, each once.
func (m *Manager) expand(specs []string) ([]string, error) {
	if len(specs) == 0 {
		return nil, api.Errorf(api.CodeInvalid, "no targets")
	}
	if len(specs) > m.limits.Targets {
		return nil, api.Errorf(api.CodeInvalid, "too many targets (%d): a watch is limited to %d", len(specs), m.limits.Targets)
	}
	for _, s := range specs {
		if s == "" || len(s) > maxSpec {
			return nil, api.Errorf(api.CodeInvalid, "targets must have 1 to %d bytes, got one with %d", maxSpec, len(s))
		}
	}
	expanded, err := target.Expand(specs, m.limits.Targets)
	if err != nil {
		return nil, api.Errorf(api.CodeInvalid, "%v", err)
	}
	seen := make(map[string]bool)
	var targets []string
	for _, t := range expanded {
		if !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	return targets, nil
}

// Create starts a new watch. It returns once the targets are resolved, all of
// them, or with the error; ctx bounds only that.
func (m *Manager) Create(ctx context.Context, req api.CreateRequest) (api.Watch, error) {
	w, targets, err := m.prepare(req)
	if err != nil {
		return api.Watch{}, err
	}
	if err := m.reserve(w); err != nil {
		return api.Watch{}, err
	}
	defer m.wg.Done()
	sess, err := m.open(ctx, w, targets)

	m.mu.Lock()
	if m.closed { // and Shutdown waits for this, so that nothing is left behind
		err = errors.Join(err, errShuttingDown)
	}
	if err != nil {
		m.remove(w)
		m.mu.Unlock()
		if sess != nil {
			sess.Stop()
		}
		return api.Watch{}, m.createError(ctx, err, len(targets))
	}
	now := m.now()
	w.state, w.sess, w.started, w.checked = api.StateRunning, sess, now.wall, now
	w.ctx, w.cancel = context.WithCancel(context.Background())
	m.setEnd(w, now, w.duration)
	w.log.add(api.Event{
		Kind: api.EventWatchStarted, Time: now.wall, RecordedAt: now.wall,
		Started: &api.Started{ExpiresAt: w.expires},
	})
	v := w.view()
	m.wg.Add(1)
	go m.run(w, sess)
	m.mu.Unlock()
	v.snapshot()
	return v.Watch, nil
}

var errShuttingDown = api.Errorf(api.CodeShuttingDown, "the server is shutting down")

// reserve registers w, which takes a slot and the name from now on. m.wg counts
// it until the creation has ended.
func (m *Manager) reserve(w *watch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errShuttingDown
	}
	for _, o := range m.watches {
		if o.name == w.name && o.state != api.StateStopped {
			return api.Errorf(api.CodeConflict, "a watch named %s has not stopped yet", w.name)
		}
	}
	if m.active() >= m.limits.Running {
		return api.Errorf(api.CodeLimit, "%d watches are running, which is the limit", m.limits.Running)
	}
	w.id = newID()
	m.watches = append(m.watches, w)
	m.wg.Add(1)
	return nil
}

// open creates the session of w.
func (m *Manager) open(ctx context.Context, w *watch, targets []string) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
	defer context.AfterFunc(m.ctx, cancel)()

	cfg, err := m.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return m.newSession(ctx, session.Params{
		Config:          cfg,
		Targets:         targets,
		SourceInterface: w.source,
		Interval:        w.interval,
		Timeout:         w.timeout,
		Stats:           stats.Options{OnEvent: w.log.addStats},
	})
}

// createError returns the error of a Create for its caller.
func (m *Manager) createError(ctx context.Context, err error, targets int) *api.Error {
	var bad prober.TargetErrors
	var proberErr prober.ProberError
	switch {
	case errors.Is(err, errShuttingDown), m.ctx.Err() != nil:
		return errShuttingDown
	case errors.As(err, &bad):
		e := api.Errorf(api.CodeUnresolvable, "%d of %d targets cannot be probed", len(bad), targets)
		if errors.As(err, &proberErr) {
			e = api.Errorf(api.CodeUnresolvable, "%s; %v", e.Message, proberErr)
		}
		for _, t := range bad {
			e.Targets = append(e.Targets, api.TargetError{
				Target: api.Clean(t.Target, maxText), Message: api.Clean(t.Err.Error(), maxText),
			})
		}
		return e
	case errors.As(err, &proberErr):
		return api.Errorf(api.CodeUnresolvable, "%v", err)
	case ctx.Err() != nil:
		return api.Errorf(api.CodeInternal, "canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return api.Errorf(api.CodeUnresolvable, "the targets were not resolved within %v", createTimeout)
	}
	return api.Errorf(api.CodeInternal, "%v", err)
}
