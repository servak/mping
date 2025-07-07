package shared

// Config manages UI settings
type Config struct {
	Title        string `yaml:"-"`
	Border       bool   `yaml:"border"`
	EnableColors bool   `yaml:"enable_colors"`
	Colors       struct {
		Header      string `yaml:"header"`
		Footer      string `yaml:"footer"`
		Success     string `yaml:"success"`
		Warning     string `yaml:"warning"`
		Error       string `yaml:"error"`
		ModalBorder string `yaml:"modal_border"`
	} `yaml:"colors"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	cfg := &Config{
		Title:        "mping",
		Border:       true,
		EnableColors: true, // Enable colors by default
	}

	// Use color names available in tview
	cfg.Colors.Header = "dodgerblue"
	cfg.Colors.Footer = "gray"
	cfg.Colors.Success = "green"
	cfg.Colors.Warning = "yellow"
	cfg.Colors.Error = "red"
	cfg.Colors.ModalBorder = "white"

	return cfg
}