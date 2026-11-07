package prober

import (
	"fmt"
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
