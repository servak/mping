package config

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
)

const DefaultICMPBody = "mping"

type Config struct {
	Prober map[string]*prober.ProberConfig `yaml:"prober"`
	UI     *ui.UIConfig                    `yaml:"ui"`
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

func DefaultConfig() *Config {
	return &Config{
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
					ExpectCode: 200,
					ExpectBody: "",
				},
			},
			string(prober.HTTPS): {
				Probe: prober.HTTPS,
				HTTP: &prober.HTTPConfig{
					ExpectCode: 200,
					ExpectBody: "",
				},
			},
			string(prober.TCP): {
				Probe: prober.TCP,
				TCP: &prober.TCPConfig{
					SourceInterface: "",
					Timeout:         "5000ms",
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
	out, _ := yaml.Marshal(c)
	return string(out)
}
