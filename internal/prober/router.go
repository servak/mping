package prober

import (
	"errors"
	"fmt"
)

// TargetRouter routes targets to appropriate probers
type TargetRouter struct {
	probers []Prober
}

// NewTargetRouter creates a new router with provided probers
func NewTargetRouter(probers []Prober) *TargetRouter {
	return &TargetRouter{
		probers: probers,
	}
}

// RouteTargets routes multiple targets and returns registration info
func (r *TargetRouter) RouteTargets(targets []string) ([]ProbeTarget, error) {
	var registrations []ProbeTarget
	
	for _, target := range targets {
		found := false
		for _, prober := range r.probers {
			probeTarget, err := prober.Accept(target)
			if err == nil {
				// Target accepted
				registrations = append(registrations, probeTarget)
				found = true
				break
			}
			if !errors.Is(err, ErrNotAccepted) {
				return nil, fmt.Errorf("prober error for %s: %w", target, err)
			}
			// ErrNotAccepted - try next prober
		}
		if !found {
			return nil, fmt.Errorf("no prober can handle target: %s", target)
		}
	}
	return registrations, nil
}

// GetActiveProbers returns probers that have accepted at least one target
func (r *TargetRouter) GetActiveProbers() []Prober {
	var active []Prober
	for _, prober := range r.probers {
		if prober.HasTargets() {
			active = append(active, prober)
		}
	}
	return active
}

