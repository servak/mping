package prober

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type (
	ProberConfig struct {
		ICMP *ICMPConfig `yaml:"icmp"`
	}

	ICMPConfig struct {
		Body     string `yaml:"body"`
		Timeout  string `yaml:"timeout"`
		Interval string `yaml:"interval"`
	}
)

func (cfg ICMPConfig) GetInterval() (time.Duration, error) {
	sinterval := "1s"
	if cfg.Interval != "" {
		sinterval = cfg.Interval
	}
	return convertToDuration(sinterval)
}

func (cfg ICMPConfig) GetTimeout() (time.Duration, error) {
	stimeout := cfg.Timeout
	if stimeout == "" {
		return cfg.GetInterval()
	}
	return convertToDuration(stimeout)
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
		return 0, fmt.Errorf("Invalid duration format")
	}
}
