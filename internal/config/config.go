package config

import (
	"os"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Prober map[string]*prober.ProberConfig `yaml:"prober"`
	UI     *ui.UIConfig                    `yaml:"ui"`
}

func DefaultConfig() *Config {
	return &Config{
		Prober: map[string]*prober.ProberConfig{
			string(prober.ICMPV4): {
				Probe: prober.ICMPV4,
				ICMP:  &prober.ICMPConfig{},
			},
			string(prober.ICMPV6): {
				Probe: prober.ICMPV6,
				ICMP:  &prober.ICMPConfig{},
			},
			string(prober.HTTP): {
				Probe: prober.HTTP,
				HTTP: &prober.HTTPConfig{
					ExpectCode: 200,
					ExpectBody: "",
				},
			},
		},
		UI: &ui.UIConfig{
			CUI: &ui.CUIConfig{
				Border: true,
			},
		},
	}
}

func Load(s string) (*Config, error) {
	cfg := DefaultConfig()
	err := yaml.Unmarshal([]byte(s), cfg)
	return cfg, err
}

func LoadFile(s string) (*Config, error) {
	out, err := os.ReadFile(s)
	if err != nil {
		return DefaultConfig(), err
	}
	return Load(string(out))
}
