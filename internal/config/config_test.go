package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/servak/mping/internal/prober"
)

func TestValidationErrors(t *testing.T) {
	t.Run("empty validation errors", func(t *testing.T) {
		ve := &ValidationErrors{}
		if ve.HasErrors() {
			t.Error("Expected no errors")
		}
		if ve.Error() != "no validation errors" {
			t.Errorf("Expected 'no validation errors', got %s", ve.Error())
		}
	})

	t.Run("single validation error", func(t *testing.T) {
		ve := &ValidationErrors{}
		ve.Add(fmt.Errorf("test error"))
		
		if !ve.HasErrors() {
			t.Error("Expected errors")
		}
		if ve.Error() != "test error" {
			t.Errorf("Expected 'test error', got %s", ve.Error())
		}
	})

	t.Run("multiple validation errors", func(t *testing.T) {
		ve := &ValidationErrors{}
		ve.Add(fmt.Errorf("error 1"))
		ve.Add(fmt.Errorf("error 2"))
		
		if !ve.HasErrors() {
			t.Error("Expected errors")
		}
		
		expected := "multiple validation errors: error 1; error 2"
		if ve.Error() != expected {
			t.Errorf("Expected '%s', got %s", expected, ve.Error())
		}
	})

	t.Run("add nil error should be ignored", func(t *testing.T) {
		ve := &ValidationErrors{}
		ve.Add(nil)
		
		if ve.HasErrors() {
			t.Error("Expected no errors when adding nil")
		}
	})
}

func TestConfigValidate(t *testing.T) {
	t.Run("valid default config", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Default config should be valid: %v", err)
		}
	})

	t.Run("invalid default prober", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Default = "nonexistent"
		
		err := cfg.Validate()
		if err == nil {
			t.Error("Expected validation error for invalid default prober")
		}
		
		if !strings.Contains(err.Error(), "default prober 'nonexistent' not found") {
			t.Errorf("Expected error about nonexistent default prober, got: %v", err)
		}
	})

	t.Run("invalid prober config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Prober["invalid"] = &prober.ProberConfig{
			Probe: prober.HTTP,
			HTTP: &prober.HTTPConfig{
				ExpectCodes: "invalid-pattern",
			},
		}
		
		err := cfg.Validate()
		if err == nil {
			t.Error("Expected validation error for invalid prober config")
		}
		
		if !strings.Contains(err.Error(), "prober 'invalid'") {
			t.Errorf("Expected error about invalid prober, got: %v", err)
		}
	})

	t.Run("multiple validation errors", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Default = "nonexistent"
		cfg.Prober["invalid1"] = &prober.ProberConfig{
			Probe: prober.HTTP,
			HTTP: &prober.HTTPConfig{
				ExpectCodes: "invalid-pattern",
			},
		}
		cfg.Prober["invalid2"] = &prober.ProberConfig{
			Probe: prober.DNS,
			DNS: &prober.DNSConfig{
				Server:     "", // Empty server should be invalid
				RecordType: "A",
				Port:       53,
			},
		}
		
		err := cfg.Validate()
		if err == nil {
			t.Error("Expected validation errors")
		}
		
		errMsg := err.Error()
		if !strings.Contains(errMsg, "multiple validation errors") {
			t.Errorf("Expected multiple validation errors message, got: %v", err)
		}
		if !strings.Contains(errMsg, "nonexistent") {
			t.Errorf("Expected error about nonexistent default, got: %v", err)
		}
	})

	t.Run("empty default prober should be valid", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Default = ""
		
		if err := cfg.Validate(); err != nil {
			t.Errorf("Empty default prober should be valid: %v", err)
		}
	})
}

func TestLoadWithValidation(t *testing.T) {
	t.Run("valid yaml should load successfully", func(t *testing.T) {
		yamlContent := `
default: icmpv4
prober:
  icmpv4:
    probe: icmpv4
    icmp:
      body: test
`
		cfg, err := Load(yamlContent)
		if err != nil {
			t.Errorf("Valid YAML should load successfully: %v", err)
		}
		if cfg == nil {
			t.Error("Config should not be nil")
		}
	})

	t.Run("invalid yaml should fail parsing", func(t *testing.T) {
		yamlContent := `
invalid: yaml: content [
`
		_, err := Load(yamlContent)
		if err == nil {
			t.Error("Invalid YAML should fail to load")
		}
	})

	t.Run("valid yaml with invalid config should fail validation", func(t *testing.T) {
		yamlContent := `
default: icmpv4
prober:
  icmpv4:
    probe: icmpv4
    icmp:
      body: test
      tos: 999  # Invalid TOS value
`
		_, err := Load(yamlContent)
		if err == nil {
			t.Error("Invalid config should fail validation")
		}
		
		if !strings.Contains(err.Error(), "invalid TOS value") {
			t.Errorf("Expected TOS validation error, got: %v", err)
		}
	})

	t.Run("valid yaml with invalid HTTP expect codes", func(t *testing.T) {
		yamlContent := `
default: http
prober:
  http:
    probe: http
    http:
      expect_codes: "200-"  # Invalid pattern
`
		_, err := Load(yamlContent)
		if err == nil {
			t.Error("Invalid HTTP config should fail validation")
		}
		
		if !strings.Contains(err.Error(), "invalid expect_codes pattern") {
			t.Errorf("Expected expect_codes validation error, got: %v", err)
		}
	})

	t.Run("valid yaml with invalid DNS config", func(t *testing.T) {
		yamlContent := `
default: dns
prober:
  dns:
    probe: dns
    dns:
      server: ""  # Empty server
      record_type: "A"
      port: 53
`
		_, err := Load(yamlContent)
		if err == nil {
			t.Error("Invalid DNS config should fail validation")
		}
		
		if !strings.Contains(err.Error(), "DNS server is required") {
			t.Errorf("Expected DNS server validation error, got: %v", err)
		}
	})
}