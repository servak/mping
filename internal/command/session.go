package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/pflag"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

// parseTimingFlags reads and validates the --interval and --timeout flags
// (both in milliseconds) shared by the mping and batch commands.
func parseTimingFlags(flags *pflag.FlagSet) (interval, timeout time.Duration, err error) {
	intervalMs, err := flags.GetInt("interval")
	if err != nil {
		return 0, 0, err
	}
	timeoutMs, err := flags.GetInt("timeout")
	if err != nil {
		return 0, 0, err
	}
	return validateTiming(intervalMs, timeoutMs)
}

// validateTiming rejects zero values and converts milliseconds to durations.
func validateTiming(intervalMs, timeoutMs int) (time.Duration, time.Duration, error) {
	switch {
	case intervalMs == 0 && timeoutMs == 0:
		return 0, 0, errors.New("both interval and timeout can't be zero")
	case intervalMs == 0:
		return 0, 0, errors.New("interval can't be zero")
	case timeoutMs == 0:
		return 0, 0, errors.New("timeout can't be zero")
	}
	return time.Duration(intervalMs) * time.Millisecond, time.Duration(timeoutMs) * time.Millisecond, nil
}

// loadConfig loads the config file, falling back to defaults when the file
// cannot be read (LoadFile returns usable defaults alongside the error).
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadFile(path)
	if cfg == nil {
		return nil, fmt.Errorf("failed to load config %q: %w", path, err)
	}
	return cfg, nil
}

// session bundles the probe manager and metrics manager wired together.
type session struct {
	probes   prober.ProbeManager
	metrics  stats.MetricsManager
	done     <-chan struct{} // closed once all in-flight metric updates settled
	interval time.Duration
	timeout  time.Duration
}

// sessionParams describes one probing session.
type sessionParams struct {
	Cfg             *config.Config
	Hosts           []string
	SourceInterface string
	Interval        time.Duration
	Timeout         time.Duration
	// Stats carries command-specific metrics options (e.g. the failure hook);
	// SettleTime is filled in by newSession.
	Stats stats.Options
}

// newSession creates the ProbeManager and MetricsManager, registers the
// targets and subscribes the metrics manager to probe events.
func newSession(p sessionParams) (*session, error) {
	p.Cfg.SetSourceInterface(p.SourceInterface)
	p.Stats.SettleTime = stats.SettleTimeFor(p.Interval, p.Timeout)

	pm := prober.NewProbeManager(p.Cfg.Prober, p.Cfg.Default)
	mm := stats.NewMetricsManagerWithOptions(p.Stats)
	if err := pm.AddTargets(p.Hosts...); err != nil {
		return nil, fmt.Errorf("failed to add targets: %w", err)
	}
	return &session{
		probes:   pm,
		metrics:  mm,
		done:     mm.Subscribe(pm.Events()),
		interval: p.Interval,
		timeout:  p.Timeout,
	}, nil
}

// start runs probing in the background. A ProbeManager.Run error is
// delivered on the returned channel (buffered, at most one value) so the
// caller decides when to report it.
func (s *session) start(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.probes.Run(ctx, s.interval, s.timeout); err != nil {
			errCh <- err
		}
	}()
	return errCh
}

// stop halts probing and waits for in-flight metric updates to settle.
func (s *session) stop() {
	s.probes.Stop()
	<-s.done
}
