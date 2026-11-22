// Package session runs one independent probing unit: the probers of a set of
// targets together with the metrics of their results.
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

// Session bundles the probe manager and metrics manager wired together.
type Session struct {
	probes   prober.ProbeManager
	metrics  stats.MetricsManager
	done     <-chan struct{} // closed once all in-flight metric updates settled
	interval time.Duration
	timeout  time.Duration

	mu      sync.Mutex
	started bool
	stopped bool
}

// The errors of a Start that does not start anything.
var (
	ErrStarted = errors.New("session was already started")
	ErrStopped = errors.New("session is stopped")
)

// Params describes one probing session.
type Params struct {
	// Config is copied by New, so sessions may share it. It must be valid
	// (see Config.Validate), which New does not check.
	Config          *config.Config
	Targets         []string // at least one
	SourceInterface string
	Interval        time.Duration // positive
	Timeout         time.Duration // positive
	// Stats carries caller-specific metrics options (e.g. the failure hook);
	// SettleTime is filled in by New.
	Stats stats.Options
}

// validate returns an error for what would fail later, or panic.
func (p *Params) validate() error {
	switch {
	case p.Config == nil:
		return errors.New("no config")
	case len(p.Targets) == 0:
		return errors.New("no targets")
	case p.Interval <= 0:
		return fmt.Errorf("interval must be greater than zero, got %v", p.Interval)
	case p.Timeout <= 0:
		return fmt.Errorf("timeout must be greater than zero, got %v", p.Timeout)
	}
	return nil
}

// New creates the ProbeManager and MetricsManager, registers the targets and
// subscribes the metrics manager to probe events. The session may hold sockets
// from here on; Stop releases them, whether it was started or not.
//
// ctx bounds the lookups of the target names only, not the session: when it is
// done New returns its error at once. Targets that cannot be probed are
// reported all together, as prober.TargetErrors, and probers that cannot be
// created as prober.ProberError. A New that fails holds nothing, except that
// lookups the system cannot interrupt end in the background.
func New(ctx context.Context, p Params) (*Session, error) {
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}
	cfg := p.Config.Clone()
	cfg.SetSourceInterface(p.SourceInterface)
	p.Stats.SettleTime = stats.SettleTimeFor(p.Interval, p.Timeout)

	pm := prober.NewProbeManager(cfg.Prober, cfg.Default)
	mm := stats.NewMetricsManagerWithOptions(p.Stats)
	if err := pm.AddTargets(ctx, p.Targets...); err != nil {
		pm.Stop()
		return nil, fmt.Errorf("failed to add targets: %w", err)
	}
	return &Session{
		probes:   pm,
		metrics:  mm,
		done:     mm.Subscribe(pm.Events()),
		interval: p.Interval,
		timeout:  p.Timeout,
	}, nil
}

// Metrics returns the metrics of the targets, which stay readable after Stop.
func (s *Session) Metrics() stats.MetricsManager { return s.metrics }

// Start runs probing in the background until ctx is done or Stop is called, or
// a prober fails to start. A session runs once: a second Start, or one after
// Stop, starts nothing and reports ErrStarted or ErrStopped. A Stop at the
// same time as the first Start is no error, whichever comes first to the
// probers.
//
// The returned channel yields at most one error and is closed when the run has
// ended, that is, when all is released and settled as after Stop (at once, if
// nothing was started). It is buffered, so it may be ignored. Receiving from
// it waits for the end of the run: nil for one that ended because it was told
// to. As with Stop, a hook in Params.Stats must not do that.
func (s *Session) Start(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	if err := s.begin(); err != nil {
		errCh <- err
		close(errCh)
		return errCh
	}
	go func() {
		defer close(errCh)
		// Run returns nil if Stop was faster
		if err := s.probes.Run(ctx, s.interval, s.timeout); err != nil {
			errCh <- err
		}
		s.Stop()
	}()
	return errCh
}

// begin marks the session as started, if it may be.
func (s *Session) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.stopped:
		return ErrStopped
	case s.started:
		return ErrStarted
	}
	s.started = true
	return nil
}

// Stop halts probing and waits until the replies to the last probes have
// arrived or timed out and all results are recorded and settled. It takes up to
// about the timeout (and the lookup by the system resolver, for NTP servers
// given by name). It may be called any number of times, also concurrently, and
// each call returns only when all is done; but not from a hook in Params.Stats,
// which it would wait for.
func (s *Session) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.probes.Stop()
	<-s.done
}
