package prober

import (
	"fmt"
	"maps"
)

type (
	ProberConfig struct {
		Probe ProbeType   `yaml:"probe"`
		ICMP  *ICMPConfig `yaml:"icmp,omitempty"`
		HTTP  *HTTPConfig `yaml:"http,omitempty"`
		TCP   *TCPConfig  `yaml:"tcp,omitempty"`
		DNS   *DNSConfig  `yaml:"dns,omitempty"`
		NTP   *NTPConfig  `yaml:"ntp,omitempty"`
	}
)

// Clone returns a deep copy of pc. A new pointer, map or slice field in any
// prober's config must be copied here (config.TestCloneSharesNothing checks).
func (pc *ProberConfig) Clone() *ProberConfig {
	c := clonePtr(pc)
	if c == nil {
		return nil
	}
	c.ICMP = clonePtr(pc.ICMP)
	c.HTTP = clonePtr(pc.HTTP)
	if h := c.HTTP; h != nil {
		h.Header = maps.Clone(h.Header)
		h.TLS = clonePtr(h.TLS)
		if h.TLS != nil {
			h.TLS.SkipVerify = clonePtr(h.TLS.SkipVerify)
		}
	}
	c.TCP = clonePtr(pc.TCP)
	c.DNS = clonePtr(pc.DNS)
	c.NTP = clonePtr(pc.NTP)
	return c
}

// clonePtr returns a pointer to a shallow copy of *p, or nil.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// Validate validates the prober configuration
func (pc *ProberConfig) Validate() error {
	switch pc.Probe {
	case ICMPV4, ICMPV6:
		if pc.ICMP == nil {
			return fmt.Errorf("ICMP config required for probe type %s", pc.Probe)
		}
		return pc.ICMP.Validate()
	case HTTP, HTTPS:
		if pc.HTTP == nil {
			return fmt.Errorf("HTTP config required for probe type %s", pc.Probe)
		}
		return pc.HTTP.Validate()
	case TCP:
		if pc.TCP == nil {
			return fmt.Errorf("TCP config required for probe type %s", pc.Probe)
		}
		return pc.TCP.Validate()
	case DNS:
		if pc.DNS == nil {
			return fmt.Errorf("DNS config required for probe type %s", pc.Probe)
		}
		return pc.DNS.Validate()
	case NTP:
		if pc.NTP == nil {
			return fmt.Errorf("NTP config required for probe type %s", pc.Probe)
		}
		return pc.NTP.Validate()
	default:
		return fmt.Errorf("unknown probe type: %s", pc.Probe)
	}
}
