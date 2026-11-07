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
	"github.com/servak/mping/internal/ui/shared"
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
	UI      *shared.Config                  `yaml:"ui"`
}

func (c *Config) SetTitle(t string) {
	c.UI.Title = t
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
				Probe: prober.HTTPS,
				HTTP: &prober.HTTPConfig{
					ExpectCodes: "200-299",
					ExpectBody:  "",
					TLS: &prober.TLSConfig{
						SkipVerify: prober.BoolPtr(true), // Default to skipping SSL verification
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
					Server:    "pool.ntp.org",
					Port:      123,
					MaxOffset: 5 * time.Second, // Alert if time drift > 5 seconds
				},
			},
		},
		UI: shared.DefaultConfig(),
	}
}

// Load parses a YAML configuration on top of DefaultConfig().
//
// Prober entries whose name matches a built-in default (icmpv4, http, https,
// dns, ...) are merged field-by-field over that default, so a partial entry
// such as `https: {http: {expect_codes: "200"}}` keeps the default tls and
// other settings. If an entry changes the probe family (e.g. `http: {probe: tcp}`)
// it replaces the default instead. New prober names are decoded as-is.
func Load(s string) (*Config, error) {
	cfg := DefaultConfig()
	defaults := cfg.Prober

	// Decode everything except the prober map normally. yaml.v3 would decode
	// each prober entry into a fresh struct (dropping defaults), so the map is
	// decoded separately below.
	cfg.Prober = nil
	if err := yaml.Unmarshal([]byte(s), cfg); err != nil {
		return nil, err
	}

	var raw struct {
		Prober map[string]yaml.Node `yaml:"prober"`
	}
	if err := yaml.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	probers, err := mergeProbers(defaults, raw.Prober)
	if err != nil {
		return nil, err
	}
	cfg.Prober = probers

	// Validate configuration after loading
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// mergeProbers decodes user prober entries over the given defaults.
// For keys present in defaults, the YAML node is decoded into the existing
// pointer: yaml.v3 reuses non-nil pointers and only overwrites fields that
// appear in the document, so nested structs (e.g. http.tls) are merged too.
func mergeProbers(defaults map[string]*prober.ProberConfig, user map[string]yaml.Node) (map[string]*prober.ProberConfig, error) {
	for name, node := range user {
		if base, ok := defaults[name]; ok && base != nil && sameProbeFamily(base.Probe, probeOf(&node, base.Probe)) {
			if err := node.Decode(base); err != nil {
				return nil, fmt.Errorf("prober '%s': %w", name, err)
			}
			continue
		}
		pc := &prober.ProberConfig{}
		if err := node.Decode(pc); err != nil {
			return nil, fmt.Errorf("prober '%s': %w", name, err)
		}
		defaults[name] = pc
	}
	return defaults, nil
}

// probeOf returns the "probe" value of a prober YAML node, or fallback if unset.
func probeOf(node *yaml.Node, fallback prober.ProbeType) prober.ProbeType {
	var v struct {
		Probe prober.ProbeType `yaml:"probe"`
	}
	if err := node.Decode(&v); err != nil || v.Probe == "" {
		return fallback
	}
	return v.Probe
}

// sameProbeFamily reports whether two probe types share a config section
// (http/https share the http section, icmpv4/icmpv6 share the icmp section).
func sameProbeFamily(a, b prober.ProbeType) bool {
	family := func(t prober.ProbeType) prober.ProbeType {
		switch t {
		case prober.HTTPS:
			return prober.HTTP
		case prober.ICMPV6:
			return prober.ICMPV4
		}
		return t
	}
	return family(a) == family(b)
}

// ExpandHome replaces a leading "~" with the current user's home directory.
// Paths without a leading "~" are returned unchanged. If the home directory
// cannot be determined, the original path is returned together with the error.
func ExpandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	usr, err := user.Current()
	if err != nil {
		return path, err
	}
	return strings.Replace(path, "~", usr.HomeDir, 1), nil
}

func LoadFile(path string) (*Config, error) {
	// A failed lookup keeps the path as given, as before.
	path, _ = ExpandHome(path)
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
