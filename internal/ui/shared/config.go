package shared

import (
	"os"
	"strings"
)

// Config manages UI settings
type Config struct {
	Title        string `yaml:"-"`
	Border       bool   `yaml:"border"`
	EnableColors bool   `yaml:"enable_colors"`
	Theme        string `yaml:"theme"`               // "auto", "light", "dark", "custom"
	Themes       map[string]Theme `yaml:"themes"`   // User-defined themes
	
	// Legacy settings for backward compatibility
	ColorScheme  string `yaml:"color_scheme,omitempty"` // "auto", "light", "dark"
	Colors       struct {
		Header      string `yaml:"header"`
		Footer      string `yaml:"footer"`
		Success     string `yaml:"success"`
		Warning     string `yaml:"warning"`
		Error       string `yaml:"error"`
		ModalBorder string `yaml:"modal_border"`
	} `yaml:"colors,omitempty"`
}

// Theme represents a color theme with direct color values
type Theme struct {
	// Base text colors
	Primary     string `yaml:"primary"`     // 主要テキスト色
	Secondary   string `yaml:"secondary"`   // 補助テキスト色
	
	// State colors
	Success     string `yaml:"success"`     // 成功状態
	Warning     string `yaml:"warning"`     // 警告状態
	Error       string `yaml:"error"`       // エラー状態
	
	// Table colors
	TableHeader string `yaml:"table_header"` // テーブルヘッダー
	
	// Interactive colors
	SelectionBg string `yaml:"selection_bg"` // 選択背景
	SelectionFg string `yaml:"selection_fg"` // 選択前景
	
	// Detail colors
	Accent      string `yaml:"accent"`      // アクセント色（ラベル用）
	Separator   string `yaml:"separator"`   // 区切り線
	Timestamp   string `yaml:"timestamp"`   // タイムスタンプ
}


// PredefinedThemes contains built-in color themes
var PredefinedThemes = map[string]Theme{
	"dark": {
		Primary:     "#ffffff",
		Secondary:   "#cccccc",
		Success:     "#00ff00",
		Warning:     "#ffff00",
		Error:       "#ff0000",
		TableHeader: "#ffff00",
		SelectionBg: "#006400",
		SelectionFg: "#ffffff",
		Accent:      "#00ffff",
		Separator:   "#666666",
		Timestamp:   "#999999",
	},
	"light": {
		Primary:     "#000000",
		Secondary:   "#333333",
		Success:     "#008000",
		Warning:     "#ff8c00",
		Error:       "#cc0000",
		TableHeader: "#000080",
		SelectionBg: "#add8e6",
		SelectionFg: "#000000",
		Accent:      "#000080",
		Separator:   "#666666",
		Timestamp:   "#666666",
	},
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	cfg := &Config{
		Title:        "mping",
		Border:       true,
		EnableColors: true, // Enable colors by default
		Theme:        "auto", // Auto-detect by default
		Themes:       make(map[string]Theme),
	}

	// Legacy settings for backward compatibility
	cfg.Colors.Header = "dodgerblue"
	cfg.Colors.Footer = "gray"
	cfg.Colors.Success = "green"
	cfg.Colors.Warning = "yellow"
	cfg.Colors.Error = "red"
	cfg.Colors.ModalBorder = "white"

	return cfg
}

// DetectColorScheme detects if the terminal is using light or dark theme
func DetectColorScheme() string {
	// Check environment variables for theme hints
	term := strings.ToLower(os.Getenv("TERM"))
	colorterm := strings.ToLower(os.Getenv("COLORTERM"))
	
	// Check for explicit light/dark theme indicators
	if strings.Contains(term, "light") || strings.Contains(colorterm, "light") {
		return "light"
	}
	if strings.Contains(term, "dark") || strings.Contains(colorterm, "dark") {
		return "dark"
	}

	// Check iTerm2 theme detection
	if os.Getenv("ITERM_PROFILE") != "" {
		// iTerm2 users can set ITERM_PROFILE to indicate theme
		profile := strings.ToLower(os.Getenv("ITERM_PROFILE"))
		if strings.Contains(profile, "light") {
			return "light"
		}
		if strings.Contains(profile, "dark") {
			return "dark"
		}
	}

	// Default to dark theme if detection fails
	return "dark"
}

// GetTheme returns the appropriate theme based on config
func (c *Config) GetTheme() *Theme {
	themeName := c.Theme
	
	// Handle auto detection
	if themeName == "auto" {
		themeName = DetectColorScheme()
	}
	
	// Check user-defined themes first
	if theme, exists := c.Themes[themeName]; exists {
		return &theme
	}
	
	// Check predefined themes
	if theme, exists := PredefinedThemes[themeName]; exists {
		return &theme
	}
	
	// Fallback to dark theme
	if theme, exists := PredefinedThemes["dark"]; exists {
		return &theme
	}
	
	// Ultimate fallback
	return &Theme{
		Primary:     "#ffffff",
		Secondary:   "#cccccc",
		Success:     "#00ff00",
		Warning:     "#ffff00",
		Error:       "#ff0000",
		TableHeader: "#ffff00",
		SelectionBg: "#006400",
		SelectionFg: "#ffffff",
		Accent:      "#00ffff",
		Separator:   "#666666",
		Timestamp:   "#999999",
	}
}

