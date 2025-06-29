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
	}
)

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
