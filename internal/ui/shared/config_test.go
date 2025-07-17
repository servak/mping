package shared

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify default values
	if cfg.Title != "mping" {
		t.Errorf("Expected title 'mping', got '%s'", cfg.Title)
	}

	// Verify predefined themes are pre-populated
	if len(cfg.Themes) == 0 {
		t.Error("Expected predefined themes to be pre-populated")
	}

	// Check that dark theme exists and has correct values
	darkTheme, exists := cfg.Themes["dark"]
	if !exists {
		t.Error("Expected dark theme to be pre-populated")
	}
	if darkTheme.Primary != "#ffffff" {
		t.Errorf("Expected dark theme primary to be '#ffffff', got '%s'", darkTheme.Primary)
	}
}

func TestConfigCustomization(t *testing.T) {
	cfg := DefaultConfig()

	// Test that we can modify config values
	cfg.Title = "custom-mping"

	if cfg.Title != "custom-mping" {
		t.Errorf("Expected title to be modifiable, got '%s'", cfg.Title)
	}
}

func TestThemeOverride(t *testing.T) {
	// Create default config with predefined themes
	cfg := DefaultConfig()

	// Verify dark theme has default primary color
	if cfg.Themes["dark"].Primary != "#ffffff" {
		t.Errorf("Expected default dark theme primary to be '#ffffff', got '%s'", cfg.Themes["dark"].Primary)
	}

	// Simulate YAML override using the new UI config structure
	yamlConfig := `
theme: mytheme
themes:
  mytheme:
    primary: "#ff0000"
    secondary: "#ff0001"
    background: "#ff0002"
    success: "#ff0003"
    warning: "#ff0004"
    error: "#ff0005"
    table_header: "#ff0006"
    selection_bg: "#ff0007"
    selection_fg: "#ff0008"
    accent: "#ff0009"
    separator: "#ff000a"
    timestamp: "#ff000b"
  dark:
    primary: "#333333"
`

	// Apply YAML override to existing config
	err := yaml.Unmarshal([]byte(yamlConfig), cfg)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Verify theme was changed
	if cfg.Theme != "mytheme" {
		t.Errorf("Expected theme to be 'mytheme', got '%s'", cfg.Theme)
	}

	// Verify custom theme was added
	myTheme, exists := cfg.Themes["mytheme"]
	if !exists {
		t.Error("Expected mytheme to be added")
	}
	if myTheme.Primary != "#ff0000" {
		t.Errorf("Expected mytheme primary to be '#ff0000', got '%s'", myTheme.Primary)
	}

	// Verify dark theme was partially overridden
	darkTheme := cfg.Themes["dark"]
	if darkTheme.Primary != "#333333" {
		t.Errorf("Expected dark theme primary to be overridden to '#333333', got '%s'", darkTheme.Primary)
	}
	// Verify other properties remain from default
	if darkTheme.Secondary != "#cccccc" {
		t.Errorf("Expected dark theme secondary to remain '#cccccc', got '%s'", darkTheme.Secondary)
	}
}

func TestCustomThemeValidation(t *testing.T) {
	cfg := DefaultConfig()

	// Test that incomplete custom theme fails validation
	yamlConfig := `
themes:
  incomplete:
    primary: "#ff0000"
    # Missing other required fields
`

	err := yaml.Unmarshal([]byte(yamlConfig), cfg)
	if err == nil {
		t.Error("Expected incomplete custom theme to fail validation")
	}
	if err != nil && !strings.Contains(fmt.Sprintf("%v", err), "custom theme 'incomplete'") {
		t.Errorf("Expected specific error message for custom theme, got: %v", err)
	}
}

func TestCompleteCustomTheme(t *testing.T) {
	cfg := DefaultConfig()

	// Test that complete custom theme passes validation
	yamlConfig := `
themes:
  complete:
    primary: "#ff0000"
    secondary: "#ff0001"
    background: "#ff0002"
    success: "#ff0003"
    warning: "#ff0004"
    error: "#ff0005"
    table_header: "#ff0006"
    selection_bg: "#ff0007"
    selection_fg: "#ff0008"
    accent: "#ff0009"
    separator: "#ff000a"
    timestamp: "#ff000b"
`

	err := yaml.Unmarshal([]byte(yamlConfig), cfg)
	if err != nil {
		t.Errorf("Complete custom theme should not fail validation: %v", err)
	}

	// Verify the theme was added correctly
	completeTheme, exists := cfg.Themes["complete"]
	if !exists {
		t.Error("Expected complete theme to be added")
	}
	if completeTheme.Primary != "#ff0000" {
		t.Errorf("Expected complete theme primary to be '#ff0000', got '%s'", completeTheme.Primary)
	}
	if completeTheme.Timestamp != "#ff000b" {
		t.Errorf("Expected complete theme timestamp to be '#ff000b', got '%s'", completeTheme.Timestamp)
	}
}

func TestPredefinedThemePartialOverride(t *testing.T) {
	cfg := DefaultConfig()

	// Test that predefined theme can be partially overridden
	yamlConfig := `
themes:
  dark:
    primary: "#333333"
    # Only primary is overridden, others should remain from default
`

	err := yaml.Unmarshal([]byte(yamlConfig), cfg)
	if err != nil {
		t.Errorf("Predefined theme partial override should not fail: %v", err)
	}

	darkTheme := cfg.Themes["dark"]
	if darkTheme.Primary != "#333333" {
		t.Errorf("Expected dark theme primary to be overridden to '#333333', got '%s'", darkTheme.Primary)
	}
	// Other fields should remain from default
	if darkTheme.Secondary != "#cccccc" {
		t.Errorf("Expected dark theme secondary to remain '#cccccc', got '%s'", darkTheme.Secondary)
	}
}

func TestGetThemeList(t *testing.T) {
	themes := GetThemeList()

	// Should contain all predefined themes
	expectedThemes := []string{
		"dark", "dracula", "github", "gruvbox", "light",
		"monokai", "nord", "solarized_dark", "solarized_light", "xoria256",
	}

	if len(themes) != len(expectedThemes) {
		t.Errorf("Expected %d themes, got %d", len(expectedThemes), len(themes))
	}

	// Should be sorted alphabetically
	for i, theme := range expectedThemes {
		if themes[i] != theme {
			t.Errorf("Expected theme at index %d to be '%s', got '%s'", i, theme, themes[i])
		}
	}
}

func TestGetAllThemeList(t *testing.T) {
	cfg := DefaultConfig()

	// Add custom theme
	cfg.Themes["custom"] = Theme{
		Primary: "#ff0000", Secondary: "#ff0001", Background: "#ff0002",
		Success: "#ff0003", Warning: "#ff0004", Error: "#ff0005",
		TableHeader: "#ff0006", SelectionBg: "#ff0007", SelectionFg: "#ff0008",
		Accent: "#ff0009", Separator: "#ff000a", Timestamp: "#ff000b",
	}

	themes := cfg.GetAllThemeList()

	// Should contain all predefined themes + custom theme
	expectedThemes := []string{
		"custom", "dark", "dracula", "github", "gruvbox", "light",
		"monokai", "nord", "solarized_dark", "solarized_light", "xoria256",
	}

	if len(themes) != len(expectedThemes) {
		t.Errorf("Expected %d themes, got %d: %v", len(expectedThemes), len(themes), themes)
	}

	// Should be sorted alphabetically
	for i, theme := range expectedThemes {
		if themes[i] != theme {
			t.Errorf("Expected theme at index %d to be '%s', got '%s'", i, theme, themes[i])
		}
	}
}
