package ui

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// デフォルト値の確認
	if cfg.Title != "mping" {
		t.Errorf("Expected title 'mping', got '%s'", cfg.Title)
	}

	if !cfg.Border {
		t.Error("Expected border to be true by default")
	}

	if !cfg.EnableColors {
		t.Error("Expected EnableColors to be true by default")
	}

	// カラー設定の確認
	expectedColors := map[string]string{
		"Header":      "dodgerblue",
		"Footer":      "gray",
		"Success":     "green",
		"Warning":     "yellow",
		"Error":       "red",
		"ModalBorder": "white",
	}

	actualColors := map[string]string{
		"Header":      cfg.Colors.Header,
		"Footer":      cfg.Colors.Footer,
		"Success":     cfg.Colors.Success,
		"Warning":     cfg.Colors.Warning,
		"Error":       cfg.Colors.Error,
		"ModalBorder": cfg.Colors.ModalBorder,
	}

	for key, expected := range expectedColors {
		if actual := actualColors[key]; actual != expected {
			t.Errorf("Expected %s color '%s', got '%s'", key, expected, actual)
		}
	}
}

func TestUIConfig_GetCUIConfig(t *testing.T) {
	tests := []struct {
		name     string
		uiConfig *UIConfig
		expected *Config
	}{
		{
			name:     "nil UIConfig returns default",
			uiConfig: nil,
			expected: DefaultConfig(),
		},
		{
			name: "nil CUI returns default",
			uiConfig: &UIConfig{
				CUI: nil,
			},
			expected: DefaultConfig(),
		},
		{
			name: "custom title is preserved",
			uiConfig: &UIConfig{
				CUI: &Config{
					Title:        "custom-mping",
					Border:       true,
					EnableColors: true,
				},
			},
			expected: &Config{
				Title:        "custom-mping",
				Border:       true,
				EnableColors: true,
				Colors: struct {
					Header      string `yaml:"header"`
					Footer      string `yaml:"footer"`
					Success     string `yaml:"success"`
					Warning     string `yaml:"warning"`
					Error       string `yaml:"error"`
					ModalBorder string `yaml:"modal_border"`
				}{
					Header:      "dodgerblue",
					Footer:      "gray",
					Success:     "green",
					Warning:     "yellow",
					Error:       "red",
					ModalBorder: "white",
				},
			},
		},
		{
			name: "custom colors are merged with defaults",
			uiConfig: &UIConfig{
				CUI: &Config{
					Border:       false,
					EnableColors: false,
					Colors: struct {
						Header      string `yaml:"header"`
						Footer      string `yaml:"footer"`
						Success     string `yaml:"success"`
						Warning     string `yaml:"warning"`
						Error       string `yaml:"error"`
						ModalBorder string `yaml:"modal_border"`
					}{
						Header: "red",
						Footer: "blue",
					},
				},
			},
			expected: &Config{
				Title:        "mping",
				Border:       false,
				EnableColors: false,
				Colors: struct {
					Header      string `yaml:"header"`
					Footer      string `yaml:"footer"`
					Success     string `yaml:"success"`
					Warning     string `yaml:"warning"`
					Error       string `yaml:"error"`
					ModalBorder string `yaml:"modal_border"`
				}{
					Header:      "red",
					Footer:      "blue",
					Success:     "green",
					Warning:     "yellow",
					Error:       "red",
					ModalBorder: "white",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.uiConfig.GetCUIConfig()

			if result.Title != tt.expected.Title {
				t.Errorf("Expected title '%s', got '%s'", tt.expected.Title, result.Title)
			}

			if result.Border != tt.expected.Border {
				t.Errorf("Expected border %v, got %v", tt.expected.Border, result.Border)
			}

			if result.EnableColors != tt.expected.EnableColors {
				t.Errorf("Expected EnableColors %v, got %v", tt.expected.EnableColors, result.EnableColors)
			}

			// カラー設定の比較
			if result.Colors.Header != tt.expected.Colors.Header {
				t.Errorf("Expected Header color '%s', got '%s'", tt.expected.Colors.Header, result.Colors.Header)
			}

			if result.Colors.Footer != tt.expected.Colors.Footer {
				t.Errorf("Expected Footer color '%s', got '%s'", tt.expected.Colors.Footer, result.Colors.Footer)
			}

			if result.Colors.Success != tt.expected.Colors.Success {
				t.Errorf("Expected Success color '%s', got '%s'", tt.expected.Colors.Success, result.Colors.Success)
			}
		})
	}
}