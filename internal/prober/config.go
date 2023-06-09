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
		ICMP  *ICMPConfig `yaml:"icmp"`
		HTTP  *HTTPConfig `yaml:"http"`
	}

	ICMPConfig struct {
		Body string `yaml:"body"`
	}

	HTTPConfig struct {
		ExpectCode int    `yaml:"expect_code"`
		ExpectBody string `yaml:"expect_body"`
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
		return 0, fmt.Errorf("Invalid duration format")
	}
}
