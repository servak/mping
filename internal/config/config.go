package config

import (
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
)

type Config struct {
	Prober *prober.ProberConfig `yaml:"prober"`
	UI     *ui.UIConfig         `yaml:"ui"`
}
