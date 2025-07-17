package shared

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// Config manages UI settings
type Config struct {
	Title  string           `yaml:"-"`
	Theme  string           `yaml:"theme"`  // "light", "dark", "custom"
	Themes map[string]Theme `yaml:"themes"` // User-defined themes
}

// UnmarshalYAML implements yaml.Unmarshaler to handle theme merging
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// Create a temporary structure to unmarshal into
	type tempConfig struct {
		Theme  string           `yaml:"theme"`
		Themes map[string]Theme `yaml:"themes"`
	}

	var temp tempConfig
	if err := value.Decode(&temp); err != nil {
		return err
	}

	// Apply theme if provided
	if temp.Theme != "" {
		c.Theme = temp.Theme
	}

	// Merge themes if provided
	if temp.Themes != nil {
		if err := c.mergeThemes(temp.Themes); err != nil {
			return err
		}
	}

	return nil
}

// Theme represents a color theme with direct color values
type Theme struct {
	// Base text colors
	Primary   string `yaml:"primary"`   // 主要テキスト色
	Secondary string `yaml:"secondary"` // 補助テキスト色

	// Background colors
	Background string `yaml:"background"` // メイン背景色

	// State colors
	Success string `yaml:"success"` // 成功状態
	Warning string `yaml:"warning"` // 警告状態
	Error   string `yaml:"error"`   // エラー状態

	// Table colors
	TableHeader string `yaml:"table_header"` // テーブルヘッダー

	// Interactive colors
	SelectionBg string `yaml:"selection_bg"` // 選択背景
	SelectionFg string `yaml:"selection_fg"` // 選択前景

	// Detail colors
	Accent    string `yaml:"accent"`    // アクセント色（ラベル用）
	Separator string `yaml:"separator"` // 区切り線
	Timestamp string `yaml:"timestamp"` // タイムスタンプ
}

// PredefinedThemes contains built-in color themes
var PredefinedThemes = map[string]Theme{
	"dark": {
		Primary:     "#ffffff",
		Secondary:   "#cccccc",
		Background:  "#000000",
		Success:     "#5FFF87",
		Warning:     "#ffff00",
		Error:       "#FF5F87",
		TableHeader: "#ffff00",
		SelectionBg: "#006400",
		SelectionFg: "#ffffff",
		Accent:      "#5FAFFF",
		Separator:   "#666666",
		Timestamp:   "#999999",
	},
	"light": {
		Primary:     "#000000",
		Secondary:   "#333333",
		Background:  "#ffffff",
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
	"monokai": {
		Primary:     "#f8f8f2",
		Secondary:   "#cfcfc2",
		Background:  "#272822",
		Success:     "#a6e22e",
		Warning:     "#fd971f",
		Error:       "#f92672",
		TableHeader: "#66d9ef",
		SelectionBg: "#49483e",
		SelectionFg: "#f8f8f2",
		Accent:      "#ae81ff",
		Separator:   "#75715e",
		Timestamp:   "#75715e",
	},
	"nord": {
		Primary:     "#d8dee9",
		Secondary:   "#e5e9f0",
		Background:  "#2e3440",
		Success:     "#a3be8c",
		Warning:     "#ebcb8b",
		Error:       "#bf616a",
		TableHeader: "#81a1c1",
		SelectionBg: "#3b4252",
		SelectionFg: "#eceff4",
		Accent:      "#88c0d0",
		Separator:   "#4c566a",
		Timestamp:   "#616e88",
	},
	"xoria256": {
		Primary:     "#d0d0d0", // Normal text
		Secondary:   "#9e9e9e", // LineNr, secondary text
		Background:  "#1c1c1c", // Normal background
		Success:     "#afdf87", // PreProc green
		Warning:     "#ffffaf", // Constant yellow
		Error:       "#df8787", // Special/Error red
		TableHeader: "#87afdf", // Statement blue
		SelectionBg: "#5f5f87", // Folded background
		SelectionFg: "#eeeeee", // Folded foreground
		Accent:      "#dfafdf", // Identifier purple
		Separator:   "#808080", // Comment gray
		Timestamp:   "#dfaf87", // Number tan
	},
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	cfg := &Config{
		Title:  "mping",
		Theme:  "dark",
		Themes: make(map[string]Theme),
	}

	// Pre-populate with predefined themes so they can be overridden in YAML
	for name, theme := range PredefinedThemes {
		cfg.Themes[name] = theme
	}

	return cfg
}

// GetTheme returns the appropriate theme based on config
func (c *Config) GetTheme() *Theme {
	themeName := c.Theme

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

// GetThemeList returns ordered list of available themes
func GetThemeList() []string {
	themes := make([]string, 0, len(PredefinedThemes))
	for name := range PredefinedThemes {
		themes = append(themes, name)
	}
	sort.Strings(themes)
	return themes
}

// GetAllThemeList returns ordered list of all available themes (predefined + user-defined)
func (c *Config) GetAllThemeList() []string {
	themes := make([]string, 0, len(c.Themes))
	for name := range c.Themes {
		themes = append(themes, name)
	}
	sort.Strings(themes)
	return themes
}

// CycleTheme cycles to the next theme in the available list
func (c *Config) CycleTheme() {
	themes := c.GetAllThemeList()
	currentIndex := -1

	// Find current theme index
	for i, theme := range themes {
		if theme == c.Theme {
			currentIndex = i
			break
		}
	}

	// Move to next theme (cycle back to first if at end)
	nextIndex := (currentIndex + 1) % len(themes)
	c.Theme = themes[nextIndex]
}

// mergeThemes merges user theme overrides with base themes
func (c *Config) mergeThemes(userThemes map[string]Theme) error {
	for name, userTheme := range userThemes {
		// Check if this is a predefined theme (allows partial override)
		if _, isPredefined := PredefinedThemes[name]; isPredefined {
			// For predefined themes, allow partial override
			var baseTheme Theme
			if existing, exists := c.Themes[name]; exists {
				baseTheme = existing
			} else {
				baseTheme = PredefinedThemes[name]
			}

			// Merge user theme with base theme
			merged := mergeTheme(baseTheme, userTheme)
			c.Themes[name] = merged
		} else {
			// For custom themes, require complete definition
			if err := validateTheme(userTheme); err != nil {
				return fmt.Errorf("custom theme '%s': %w", name, err)
			}
			c.Themes[name] = userTheme
		}
	}
	return nil
}

// mergeTheme merges user theme settings with base theme, preserving non-empty values
func mergeTheme(base, user Theme) Theme {
	result := base // Start with base theme

	// Override with non-empty user values
	if user.Primary != "" {
		result.Primary = user.Primary
	}
	if user.Secondary != "" {
		result.Secondary = user.Secondary
	}
	if user.Background != "" {
		result.Background = user.Background
	}
	if user.Success != "" {
		result.Success = user.Success
	}
	if user.Warning != "" {
		result.Warning = user.Warning
	}
	if user.Error != "" {
		result.Error = user.Error
	}
	if user.TableHeader != "" {
		result.TableHeader = user.TableHeader
	}
	if user.SelectionBg != "" {
		result.SelectionBg = user.SelectionBg
	}
	if user.SelectionFg != "" {
		result.SelectionFg = user.SelectionFg
	}
	if user.Accent != "" {
		result.Accent = user.Accent
	}
	if user.Separator != "" {
		result.Separator = user.Separator
	}
	if user.Timestamp != "" {
		result.Timestamp = user.Timestamp
	}

	return result
}

// validateTheme checks if a theme has all required fields defined
func validateTheme(theme Theme) error {
	if theme.Primary == "" {
		return fmt.Errorf("primary color is required")
	}
	if theme.Secondary == "" {
		return fmt.Errorf("secondary color is required")
	}
	if theme.Background == "" {
		return fmt.Errorf("background color is required")
	}
	if theme.Success == "" {
		return fmt.Errorf("success color is required")
	}
	if theme.Warning == "" {
		return fmt.Errorf("warning color is required")
	}
	if theme.Error == "" {
		return fmt.Errorf("error color is required")
	}
	if theme.TableHeader == "" {
		return fmt.Errorf("table_header color is required")
	}
	if theme.SelectionBg == "" {
		return fmt.Errorf("selection_bg color is required")
	}
	if theme.SelectionFg == "" {
		return fmt.Errorf("selection_fg color is required")
	}
	if theme.Accent == "" {
		return fmt.Errorf("accent color is required")
	}
	if theme.Separator == "" {
		return fmt.Errorf("separator color is required")
	}
	if theme.Timestamp == "" {
		return fmt.Errorf("timestamp color is required")
	}
	return nil
}
