package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
)

const DefaultICMPBody = "mping"

// ValidationErrors holds multiple validation errors
type ValidationErrors struct {
	Errors []error
}

func (ve *ValidationErrors) Error() string {
	if len(ve.Errors) == 0 {
		return "no validation errors"
	}
	if len(ve.Errors) == 1 {
		return ve.Errors[0].Error()
	}

	var msgs []string
	for _, err := range ve.Errors {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("multiple validation errors: %s", strings.Join(msgs, "; "))
}

func (ve *ValidationErrors) Add(err error) {
	if err != nil {
		ve.Errors = append(ve.Errors, err)
	}
}

func (ve *ValidationErrors) HasErrors() bool {
	return len(ve.Errors) > 0
}

type Config struct {
	Prober  map[string]*prober.ProberConfig `yaml:"prober"`
	Default string                          `yaml:"default"`
	UI      *ui.UIConfig                    `yaml:"ui"`
}

func (c *Config) SetTitle(t string) {
	c.UI.CUI.Title = t
}

func (c *Config) SetSourceInterface(sourceInterface string) {
	if sourceInterface != "" {
		for _, prober := range c.Prober {
			if prober.ICMP != nil {
				prober.ICMP.SourceInterface = sourceInterface
			}
		}
	}
}

// Validate validates the entire configuration
func (c *Config) Validate() error {
	ve := &ValidationErrors{}

	// Validate each prober configuration
	for name, proberConfig := range c.Prober {
		if err := proberConfig.Validate(); err != nil {
			ve.Add(fmt.Errorf("prober '%s': %w", name, err))
		}
	}

	// Validate default prober exists
	if c.Default != "" {
		if _, exists := c.Prober[c.Default]; !exists {
			ve.Add(fmt.Errorf("default prober '%s' not found in prober configurations", c.Default))
		}
	}

	if ve.HasErrors() {
		return ve
	}
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Default: string(prober.ICMPV4),
		Prober: map[string]*prober.ProberConfig{
			string(prober.ICMPV4): {
				Probe: prober.ICMPV4,
				ICMP: &prober.ICMPConfig{
					Body: DefaultICMPBody,
				},
			},
			string(prober.ICMPV6): {
				Probe: prober.ICMPV6,
				ICMP: &prober.ICMPConfig{
					Body: DefaultICMPBody,
				},
			},
			string(prober.HTTP): {
				Probe: prober.HTTP,
				HTTP: &prober.HTTPConfig{
					ExpectCodes: "200-299",
					ExpectBody:  "",
				},
			},
			string(prober.HTTPS): {
				Probe: prober.HTTP,
				HTTP: &prober.HTTPConfig{
					ExpectCodes: "200-299",
					ExpectBody:  "",
					TLS: &prober.TLSConfig{
						SkipVerify: true, // Default to skipping SSL verification
					},
				},
			},
			string(prober.TCP): {
				Probe: prober.TCP,
				TCP: &prober.TCPConfig{
					SourceInterface: "",
				},
			},
			string(prober.DNS): {
				Probe: prober.DNS,
				DNS: &prober.DNSConfig{
					Server:           "8.8.8.8",
					Port:             53,
					RecordType:       "A",
					UseTCP:           false,
					RecursionDesired: true, // Default to recursive queries
					ExpectCodes:      "0",  // Default to accepting only successful responses
				},
			},
			string(prober.NTP): {
				Probe: prober.NTP,
				NTP: &prober.NTPConfig{
					Port:      123,
					MaxOffset: 5 * time.Second, // Alert if time drift > 5 seconds
				},
			},
		},
		UI: &ui.UIConfig{
			CUI: ui.DefaultConfig(),
		},
	}
}

func Load(s string) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(s), cfg); err != nil {
		return nil, err
	}

	// Validate configuration after loading
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func LoadFile(path string) (*Config, error) {
	if strings.HasPrefix(path, "~") {
		usr, err := user.Current()
		if err == nil {
			path = strings.Replace(path, "~", usr.HomeDir, 1)
		}
	}
	cfgPath, _ := filepath.Abs(path)
	out, err := os.ReadFile(cfgPath)
	if err != nil {
		return DefaultConfig(), err
	}
	return Load(string(out))
}

func Marshal(c *Config) string {
	var out strings.Builder
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	_ = encoder.Encode(c)
	return out.String()
}
