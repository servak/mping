package prober

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type (
	ProberConfig struct {
		Probe ProbeType   `yaml:"probe"`
		ICMP  *ICMPConfig `yaml:"icmp,omitempty"`
		HTTP  *HTTPConfig `yaml:"http,omitempty"`
		TCP   *TCPConfig  `yaml:"tcp,omitempty"`
		DNS   *DNSConfig  `yaml:"dns,omitempty"`
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
	default:
		return fmt.Errorf("unknown probe type: %s", pc.Probe)
	}
}

func convertToDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "ms") {
		milliseconds, err := strconv.Atoi(strings.TrimSuffix(s, "ms"))
		if err != nil {
			return 0, err
		}
		return time.Duration(milliseconds) * time.Millisecond, nil
	} else if strings.HasSuffix(s, "s") {
		seconds, err := strconv.Atoi(strings.TrimSuffix(s, "s"))
		if err != nil {
			return 0, err
		}
		return time.Duration(seconds) * time.Second, nil
	} else {
		return 0, fmt.Errorf("invalid duration format")
	}
}
