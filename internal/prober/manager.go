package prober

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProbeManager manages the lifecycle of probing operations
type ProbeManager interface {
	AddTargets(ctx context.Context, targets ...string) error
	Run(ctx context.Context, interval, timeout time.Duration) error
	Events() <-chan *Event
	Stop()
}

// probeManager implements ProbeManager interface. A manager runs once: Run
// may be called at most once, and Stop (before, during or after Run) ends it
// for good and closes the event channel.
type probeManager struct {
	config      map[string]*ProberConfig
	defaultType string
	resolver    resolver
	addMu       sync.Mutex // serialises AddTargets; taken before mu
	probers     map[string]Prober
	eventChan   chan *Event
	wg          sync.WaitGroup
	mu          sync.Mutex
	started     bool // Run has started the probers
	stopped     bool // Stop has been called; the event channel is closed
	cancel      context.CancelFunc
}

// NewProbeManager creates a new ProbeManager instance
func NewProbeManager(proberConfigs map[string]*ProberConfig, defaultType string) ProbeManager {
	return &probeManager{
		config:      proberConfigs,
		defaultType: defaultType,
		resolver:    net.DefaultResolver,
		eventChan:   make(chan *Event, 1000), // Buffered channel for events
		probers:     make(map[string]Prober),
	}
}

// maxLookups is how many targets AddTargets resolves at the same time.
const maxLookups = 32

// candidate is a target that AddTargets is to add.
type candidate struct {
	target     string // as given
	routed     string // with the scheme of its prober
	proberType string
	prober     Prober
	plain      bool   // given without a scheme
	add        func() // adds the target to its prober; nil if err
	err        error
}

// AddTargets adds targets to the probers of their types: all of them, or none
// if it fails. Names are resolved concurrently, but the outcome is that of
// adding the targets one by one in the given order.
//
// Every target that cannot be probed is reported, as TargetErrors. A prober
// that cannot be created is not the fault of its targets: it is reported once,
// as ProberError, and its targets are not checked. Errors of both kinds are
// joined.
//
// When ctx is done AddTargets returns its error at once, even if a lookup
// hangs. Stop does not wait for AddTargets, which then fails.
func (pm *probeManager) AddTargets(ctx context.Context, targets ...string) (err error) {
	pm.addMu.Lock()
	defer pm.addMu.Unlock()

	pm.mu.Lock()
	err = pm.checkAddable()
	probers := maps.Clone(pm.probers)
	pm.mu.Unlock()
	if err != nil {
		return err
	}

	// Nobody knows the new probers until the targets are added, so they have
	// to be released here if that fails
	created := make(map[string]Prober)
	defer func() {
		if err != nil {
			for _, p := range created {
				p.Stop()
			}
		}
	}()

	var errs []error
	candidates := make([]*candidate, 0, len(targets))
	for _, target := range targets {
		c := &candidate{target: target, plain: !hasScheme(target, pm.config)}
		c.routed, c.proberType = pm.transformTarget(target)
		if c.proberType == "" {
			c.err = fmt.Errorf("unable to determine prober type for target: %s", target)
			candidates = append(candidates, c)
			continue
		}
		p, known := probers[c.proberType]
		if !known {
			var err error
			if p, err = pm.newProber(c.proberType); err != nil {
				errs = append(errs, ProberError{Prober: c.proberType, Err: err})
			} else {
				created[c.proberType] = p
			}
			probers[c.proberType] = p // nil if it failed, so that it is not tried again
		}
		if p != nil {
			c.prober = p
			candidates = append(candidates, c)
		}
	}

	if err := prepareAll(ctx, pm.resolver, candidates); err != nil {
		return err
	}
	if failed := targetErrors(candidates); len(failed) > 0 {
		errs = append(errs, failed)
	}
	switch len(errs) {
	case 0:
	case 1:
		return errs[0]
	default:
		return errors.Join(errs...)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()
	if err := pm.checkAddable(); err != nil {
		return err
	}
	for _, c := range candidates {
		c.add()
	}
	maps.Copy(pm.probers, created)
	return nil
}

// checkAddable returns an error if targets can no longer be added. pm.mu must
// be held.
func (pm *probeManager) checkAddable() error {
	if pm.started {
		return fmt.Errorf("ProbeManager is already running")
	}
	if pm.stopped {
		return fmt.Errorf("ProbeManager is stopped")
	}
	return nil
}

// prepareAll prepares the candidates that have not failed yet, maxLookups at a
// time. It returns the error of ctx as soon as that is done, after which the
// candidates must not be used: lookups that do not return yet may still write
// to them.
func prepareAll(ctx context.Context, r resolver, candidates []*candidate) error {
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(maxLookups, len(candidates)) {
		workers.Go(func() {
			for ctx.Err() == nil {
				i := int(next.Add(1)) - 1
				if i >= len(candidates) {
					return
				}
				c := candidates[i]
				if c.err != nil {
					continue
				}
				if c.add, c.err = c.prober.prepare(ctx, r, c.routed, c.plain); c.err != nil {
					c.err = fmt.Errorf("prober %s rejected target %s: %w", c.proberType, c.routed, c.err)
				}
			}
		})
	}
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return ctx.Err()
}

// targetErrors returns the errors of the candidates, in their order. A target
// that was given several times is reported once.
func targetErrors(candidates []*candidate) TargetErrors {
	var errs TargetErrors
	seen := make(map[string]bool)
	for _, c := range candidates {
		if c.err != nil && !seen[c.target] {
			seen[c.target] = true
			errs = append(errs, TargetError{Target: c.target, Err: c.err})
		}
	}
	return errs
}

// Run starts the probers and blocks until ctx is done or Stop is called. It
// returns nil at once when Stop was called before.
//
// A prober that cannot start ends the run, rather than leave its targets
// unprobed: Run stops all probers and returns the errors of those that failed.
// (The only error of the probers of this package is a second Start, which a
// manager never makes.)
func (pm *probeManager) Run(ctx context.Context, interval, timeout time.Duration) error {
	pm.mu.Lock()
	if pm.started {
		pm.mu.Unlock()
		return fmt.Errorf("ProbeManager is already running")
	}
	if pm.stopped {
		pm.mu.Unlock()
		return nil
	}
	if len(pm.probers) == 0 {
		pm.mu.Unlock()
		return fmt.Errorf("no probers with targets")
	}
	pm.started = true

	// Create cancelable context for this run
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pm.cancel = cancel

	// Start all probers under the lock, so that a concurrent Stop either
	// sees none of them or waits for all of them
	names := slices.Sorted(maps.Keys(pm.probers))
	errs := make([]error, len(names))
	for i, name := range names {
		p := pm.probers[name]
		pm.wg.Go(func() {
			if err := p.Start(pm.eventChan, interval, timeout); err != nil {
				errs[i] = fmt.Errorf("prober %s: %w", name, err)
				cancel()
			}
		})
	}
	pm.mu.Unlock()

	// Wait for context cancellation (Stop and a failed prober cancel it too)
	<-runCtx.Done()
	pm.Stop() // waits for all probers, so errs is complete

	return errors.Join(errs...)
}

// Events returns the event channel for receiving probe results
func (pm *probeManager) Events() <-chan *Event {
	return pm.eventChan
}

// Stop stops all probers, waits for them to finish and closes the event
// channel. It may be called before Run and any number of times, also
// concurrently: each call returns only when all of that is done.
func (pm *probeManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.stopped {
		return
	}
	pm.stopped = true

	// Cancel context to signal stop
	if pm.cancel != nil {
		pm.cancel()
	}

	// Stop all probers concurrently: each may wait for its last replies.
	// Probers that never started just release their resources.
	var stops sync.WaitGroup
	for _, prober := range pm.probers {
		stops.Go(prober.Stop)
	}
	stops.Wait()

	// Wait for goroutines to finish
	pm.wg.Wait()

	// Close event channel
	close(pm.eventChan)
}

// hasScheme reports whether target names its prober: "name://target", or
// the legacy "name:target" with a configured name.
func hasScheme(target string, config map[string]*ProberConfig) bool {
	if strings.Contains(target, "://") {
		return true
	}
	if prefix, _, ok := strings.Cut(target, ":"); ok {
		_, exists := config[prefix]
		return exists
	}
	return false
}

// transformTarget transforms target with appropriate prefix and returns (transformedTarget, proberType)
func (pm *probeManager) transformTarget(target string) (string, string) {
	// Check for name://target format
	if strings.Contains(target, "://") {
		prefix := strings.SplitN(target, "://", 2)[0]
		if _, exists := pm.config[prefix]; exists {
			return target, prefix // Already has prefix, return as-is
		}
		// If prefix not in config, return empty prober type (will cause error)
		return target, ""
	}

	// Check for legacy colon format (icmpv4:target, icmpv6:target) - convert to new format
	if strings.Contains(target, ":") && !strings.Contains(target, "://") {
		parts := strings.SplitN(target, ":", 2)
		prefix := parts[0]
		hostname := parts[1]
		if _, exists := pm.config[prefix]; exists {
			// Convert to new format: prefix://hostname
			return prefix + "://" + hostname, prefix
		}
	}

	// Plain hostname/IP - add default prefix
	if pm.defaultType != "" {
		if _, exists := pm.config[pm.defaultType]; exists {
			// Always use name://target format
			return pm.defaultType + "://" + target, pm.defaultType
		}
	}

	// No suitable prober found
	return target, ""
}

// newProber creates the prober of a type (a name in the config).
func (pm *probeManager) newProber(proberType string) (Prober, error) {
	config, exists := pm.config[proberType]
	if !exists {
		return nil, fmt.Errorf("no configuration found for prober type: %s", proberType)
	}

	// Create new prober based on config.Probe field (not the proberType name)
	switch config.Probe {
	case ICMPV4, ICMPV6:
		p, err := NewICMPProber(config.Probe, config.ICMP, proberType)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s prober: %w", proberType, err)
		}
		return p, nil
	case HTTP, HTTPS: // Both HTTP and HTTPS use HTTPProber (scheme rules: see HTTPProber.resolveURL)
		return NewHTTPProber(config.Probe, config.HTTP, proberType), nil
	case TCP:
		return NewTCPProber(config.TCP, proberType), nil
	case DNS:
		return NewDNSProber(config.DNS, proberType), nil
	case NTP:
		return NewNTPProber(config.NTP, proberType), nil
	default:
		return nil, fmt.Errorf("unknown probe type in config: %s", config.Probe)
	}
}
