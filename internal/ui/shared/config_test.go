package shared

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify default values
	if cfg.Title != "mping" {
		t.Errorf("Expected title 'mping', got '%s'", cfg.Title)
	}

	if !cfg.Border {
		t.Error("Expected border to be true by default")
	}

	if !cfg.EnableColors {
		t.Error("Expected EnableColors to be true by default")
	}

	// Verify color settings
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

func TestConfigCustomization(t *testing.T) {
	cfg := DefaultConfig()

	// Test that we can modify config values
	cfg.Title = "custom-mping"
	cfg.Border = false
	cfg.EnableColors = false
	cfg.Colors.Header = "red"

	if cfg.Title != "custom-mping" {
		t.Errorf("Expected title to be modifiable, got '%s'", cfg.Title)
	}

	if cfg.Border {
		t.Error("Expected border to be modifiable")
	}

	if cfg.EnableColors {
		t.Error("Expected EnableColors to be modifiable")
	}

	if cfg.Colors.Header != "red" {
		t.Errorf("Expected header color to be modifiable, got '%s'", cfg.Colors.Header)
	}
}