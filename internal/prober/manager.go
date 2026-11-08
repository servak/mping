package prober

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProbeManager manages the lifecycle of probing operations
type ProbeManager interface {
	AddTargets(targets ...string) error
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
		eventChan:   make(chan *Event, 1000), // Buffered channel for events
		probers:     make(map[string]Prober),
	}
}

// AddTargets adds targets to appropriate probers
func (pm *probeManager) AddTargets(targets ...string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return fmt.Errorf("ProbeManager is already running")
	}
	if pm.stopped {
		return fmt.Errorf("ProbeManager is stopped")
	}

	// Route each target to appropriate prober
	for _, target := range targets {
		if err := pm.routeTarget(target); err != nil {
			return fmt.Errorf("failed to route target %s: %w", target, err)
		}
	}

	return nil
}

// Run starts the probers and blocks until ctx is done or Stop is called. It
// returns nil at once when Stop was called before.
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
	pm.cancel = cancel

	// Start all probers under the lock, so that a concurrent Stop either
	// sees none of them or waits for all of them
	for _, prober := range pm.probers {
		pm.wg.Add(1)
		go func(p Prober) {
			defer pm.wg.Done()
			p.Start(pm.eventChan, interval, timeout)
		}(prober)
	}
	pm.mu.Unlock()

	// Wait for context cancellation (Stop cancels it too)
	<-runCtx.Done()
	pm.Stop()

	return nil
}

// Events returns the event channel for receiving probe results
func (pm *probeManager) Events() <-chan *Event {
	return pm.eventChan
}

// Stop stops all probers, waits for them to finish and closes the event
// channel. It may be called before Run and any number of times.
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

// routeTarget routes a single target to appropriate prober, creating prober if needed
func (pm *probeManager) routeTarget(target string) error {
	// Transform target with appropriate prefix
	transformedTarget, proberType := pm.transformTarget(target)
	if proberType == "" {
		return fmt.Errorf("unable to determine prober type for target: %s", target)
	}

	// Get or create prober for this type
	prober, err := pm.getOrCreateProber(proberType)
	if err != nil {
		return fmt.Errorf("failed to get prober for %s: %w", proberType, err)
	}

	// Accept transformed target in prober (prober handles its own prefix).
	// A target written without a scheme went to the default prober; one
	// that shows such targets differently is told so.
	if pa, ok := prober.(plainAccepter); ok && !hasScheme(target, pm.config) {
		err = pa.acceptPlain(transformedTarget)
	} else {
		err = prober.Accept(transformedTarget)
	}
	if err != nil {
		return fmt.Errorf("prober %s rejected target %s: %w", proberType, transformedTarget, err)
	}

	return nil
}

// plainAccepter is implemented by probers that name the targets written
// without a scheme (and routed to them as the default prober) differently
// from those written with their scheme.
type plainAccepter interface {
	acceptPlain(target string) error
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

// getOrCreateProber gets existing prober or creates new one
func (pm *probeManager) getOrCreateProber(proberType string) (Prober, error) {
	// Check if prober already exists
	if prober, exists := pm.probers[proberType]; exists {
		return prober, nil
	}

	// Get config for this prober type
	config, exists := pm.config[proberType]
	if !exists {
		return nil, fmt.Errorf("no configuration found for prober type: %s", proberType)
	}

	// Create new prober based on config.Probe field (not the proberType name)
	var prober Prober
	var err error

	switch config.Probe {
	case ICMPV4:
		prober, err = NewICMPProber(ICMPV4, config.ICMP, proberType)
	case ICMPV6:
		prober, err = NewICMPProber(ICMPV6, config.ICMP, proberType)
	case HTTP, HTTPS: // Both HTTP and HTTPS use HTTPProber (scheme rules: see HTTPProber.resolveURL)
		prober = NewHTTPProber(config.Probe, config.HTTP, proberType)
	case TCP:
		prober = NewTCPProber(config.TCP, proberType)
	case DNS:
		prober = NewDNSProber(config.DNS, proberType)
	case NTP:
		prober = NewNTPProber(config.NTP, proberType)
	default:
		return nil, fmt.Errorf("unknown probe type in config: %s", config.Probe)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s prober: %w", proberType, err)
	}

	// Store the prober
	pm.probers[proberType] = prober
	return prober, nil
}
