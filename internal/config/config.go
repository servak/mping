package config

import (
	"os"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Prober *prober.ProberConfig `yaml:"prober"`
	UI     *ui.UIConfig         `yaml:"ui"`
}

func DefaultConfig() *Config {
	return &Config{
		Prober: &prober.ProberConfig{
			ICMP: &prober.ICMPConfig{
				Interval: "1s",
				Timeout:  "1s",
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
