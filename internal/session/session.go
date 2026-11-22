// Package session runs one independent probing unit: the probers of a set of
// targets together with the metrics of their results.
package session

import (
	"context"
	"fmt"
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
}

// Params describes one probing session.
type Params struct {
	Config          *config.Config
	Targets         []string
	SourceInterface string
	Interval        time.Duration
	Timeout         time.Duration
	// Stats carries caller-specific metrics options (e.g. the failure hook);
	// SettleTime is filled in by New.
	Stats stats.Options
}

// New creates the ProbeManager and MetricsManager, registers the targets and
// subscribes the metrics manager to probe events.
func New(p Params) (*Session, error) {
	p.Config.SetSourceInterface(p.SourceInterface)
	p.Stats.SettleTime = stats.SettleTimeFor(p.Interval, p.Timeout)

	pm := prober.NewProbeManager(p.Config.Prober, p.Config.Default)
	mm := stats.NewMetricsManagerWithOptions(p.Stats)
	if err := pm.AddTargets(p.Targets...); err != nil {
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

// Start runs probing in the background. A ProbeManager.Run error is
// delivered on the returned channel (buffered, at most one value) so the
// caller decides when to report it.
func (s *Session) Start(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.probes.Run(ctx, s.interval, s.timeout); err != nil {
			errCh <- err
		}
	}()
	return errCh
}

// Stop halts probing and waits for in-flight metric updates to settle.
func (s *Session) Stop() {
	s.probes.Stop()
	<-s.done
}
