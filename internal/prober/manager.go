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

// probeManager implements ProbeManager interface
type probeManager struct {
	config    map[string]*ProberConfig
	probers   map[string]Prober
	eventChan chan *Event
	wg        sync.WaitGroup
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
}

// NewProbeManager creates a new ProbeManager instance
func NewProbeManager(proberConfigs map[string]*ProberConfig) ProbeManager {
	return &probeManager{
		config:    proberConfigs,
		eventChan: make(chan *Event, 1000), // Buffered channel for events
		probers:   make(map[string]Prober),
	}
}

// AddTargets adds targets to appropriate probers
func (pm *probeManager) AddTargets(targets ...string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return fmt.Errorf("ProbeManager is already running")
	}

	// Route each target to appropriate prober
	for _, target := range targets {
		if err := pm.routeTarget(target); err != nil {
			return fmt.Errorf("failed to route target %s: %w", target, err)
		}
	}

	return nil
}

// Run starts the probing operations
func (pm *probeManager) Run(ctx context.Context, interval, timeout time.Duration) error {
	pm.mu.Lock()
	if pm.running {
		pm.mu.Unlock()
		return fmt.Errorf("ProbeManager is already running")
	}
	pm.running = true

	// Check if any probers exist
	if len(pm.probers) == 0 {
		pm.mu.Unlock()
		return fmt.Errorf("no probers with targets")
	}

	// Create cancelable context for this run
	runCtx, cancel := context.WithCancel(ctx)
	pm.cancel = cancel
	pm.mu.Unlock()

	// Start all probers
	for _, prober := range pm.probers {
		pm.wg.Add(1)
		go func(p Prober) {
			defer pm.wg.Done()
			p.Start(pm.eventChan, interval, timeout)
		}(prober)
	}

	// Wait for context cancellation
	<-runCtx.Done()
	pm.Stop()

	return nil
}

// Events returns the event channel for receiving probe results
func (pm *probeManager) Events() <-chan *Event {
	return pm.eventChan
}

// Stop stops all probing operations
func (pm *probeManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return
	}

	// Cancel context to signal stop
	if pm.cancel != nil {
		pm.cancel()
	}

	// Stop all probers
	for _, prober := range pm.probers {
		prober.Stop()
	}

	// Wait for goroutines to finish
	pm.wg.Wait()

	// Close event channel
	close(pm.eventChan)

	pm.running = false
}


// routeTarget routes a single target to appropriate prober, creating prober if needed
func (pm *probeManager) routeTarget(target string) error {
	// Determine prober type from target
	proberType := pm.determineProberType(target)
	
	// Get or create prober for this type
	prober, err := pm.getOrCreateProber(proberType)
	if err != nil {
		return fmt.Errorf("failed to get prober for %s: %w", proberType, err)
	}
	
	// Accept target in prober
	err = prober.Accept(target)
	if err != nil {
		return fmt.Errorf("prober %s rejected target %s: %w", proberType, target, err)
	}
	
	return nil
}

// determineProberType determines the prober type from target string
func (pm *probeManager) determineProberType(target string) string {
	// Legacy protocol prefixes
	if strings.HasPrefix(target, "icmpv4:") {
		return string(ICMPV4)
	}
	if strings.HasPrefix(target, "icmpv6:") {
		return string(ICMPV6)
	}
	if strings.HasPrefix(target, "http://") {
		return string(HTTP)
	}
	if strings.HasPrefix(target, "https://") {
		return string(HTTPS)
	}
	if strings.HasPrefix(target, "tcp://") {
		return string(TCP)
	}
	if strings.HasPrefix(target, "dns://") {
		return string(DNS)
	}
	
	// Default to ICMPv4 for plain hostnames/IPs
	return string(ICMPV4)
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
	
	// Create new prober based on type
	var prober Prober
	var err error
	
	switch ProbeType(proberType) {
	case ICMPV4:
		prober, err = NewICMPProber(ICMPV4, config.ICMP)
	case ICMPV6:
		prober, err = NewICMPProber(ICMPV6, config.ICMP)
	case HTTP, HTTPS:
		prober = NewHTTPProber(config.HTTP)
	case TCP:
		prober = NewTCPProber(config.TCP)
	case DNS:
		prober = NewDNSProber(config.DNS)
	default:
		return nil, fmt.Errorf("unknown prober type: %s", proberType)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to create %s prober: %w", proberType, err)
	}
	
	// Store the prober
	pm.probers[proberType] = prober
	return prober, nil
}

