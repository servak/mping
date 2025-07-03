package prober

import (
	"testing"
)

func TestProbeManagerDetailedScenarios(t *testing.T) {
	config := map[string]*ProberConfig{
		"http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"my-secure": {
			Probe: HTTP,
			HTTP: &HTTPConfig{
				ExpectCode: 200,
				TLS:        &TLSConfig{SkipVerify: true},
			},
		},
	}

	t.Run("Events channel availability", func(t *testing.T) {
		pm := NewProbeManager(config, "http")
		events := pm.Events()
		if events == nil {
			t.Error("Events channel should not be nil")
		}
	})

	t.Run("AddTargets with running manager", func(t *testing.T) {
		pm := NewProbeManager(config, "http")

		// Add initial targets
		err := pm.AddTargets("http://example.com")
		if err != nil {
			t.Fatalf("Failed to add targets: %v", err)
		}

		// Simulate running state
		pm.(*probeManager).running = true

		// Try to add more targets while running
		err = pm.AddTargets("http://google.com")
		if err == nil {
			t.Error("Expected error when adding targets to running manager")
		}
	})

	t.Run("Multiple targets same prober", func(t *testing.T) {
		pm := NewProbeManager(config, "http")

		targets := []string{
			"http://example.com",
			"http://google.com",
			"http://github.com",
		}

		err := pm.AddTargets(targets...)
		if err != nil {
			t.Errorf("Failed to add multiple targets: %v", err)
		}
	})

	t.Run("Mixed prober types", func(t *testing.T) {
		pm := NewProbeManager(config, "http")

		targets := []string{
			"example.com",         // Should use default (http)
			"http://example.com",  // HTTP prober
			"my-secure://api.com", // HTTPS prober with TLS
		}

		err := pm.AddTargets(targets...)
		if err != nil {
			t.Errorf("Failed to add mixed targets: %v", err)
		}
	})

	t.Run("Invalid default prober", func(t *testing.T) {
		pm := NewProbeManager(config, "nonexistent")

		err := pm.AddTargets("example.com")
		if err == nil {
			t.Error("Expected error with invalid default prober")
		}
	})

	t.Run("Empty config", func(t *testing.T) {
		emptyConfig := map[string]*ProberConfig{}
		pm := NewProbeManager(emptyConfig, "http")

		err := pm.AddTargets("example.com")
		if err == nil {
			t.Error("Expected error with empty config")
		}
	})
}

func TestGetOrCreateProber(t *testing.T) {
	config := map[string]*ProberConfig{
		"http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"https": {
			Probe: HTTP,
			HTTP: &HTTPConfig{
				ExpectCode: 200,
				TLS:        &TLSConfig{SkipVerify: true},
			},
		},
		"tcp": {
			Probe: TCP,
			TCP:   &TCPConfig{},
		},
	}

	pm := &probeManager{
		config:  config,
		probers: make(map[string]Prober),
	}

	t.Run("Create new prober", func(t *testing.T) {
		prober, err := pm.getOrCreateProber("http")
		if err != nil {
			t.Errorf("Failed to create HTTP prober: %v", err)
		}
		if prober == nil {
			t.Error("Prober should not be nil")
		}
	})

	t.Run("Reuse existing prober", func(t *testing.T) {
		// First call creates prober
		prober1, err := pm.getOrCreateProber("http")
		if err != nil {
			t.Errorf("Failed to create HTTP prober: %v", err)
		}

		// Second call should return same prober
		prober2, err := pm.getOrCreateProber("http")
		if err != nil {
			t.Errorf("Failed to get existing HTTP prober: %v", err)
		}

		if prober1 != prober2 {
			t.Error("Should reuse existing prober instance")
		}
	})

	t.Run("Unknown prober type", func(t *testing.T) {
		_, err := pm.getOrCreateProber("unknown")
		if err == nil {
			t.Error("Expected error for unknown prober type")
		}
	})

	t.Run("Invalid probe config", func(t *testing.T) {
		invalidConfig := map[string]*ProberConfig{
			"invalid": {
				Probe: "invalid-type",
			},
		}

		pm := &probeManager{
			config:  invalidConfig,
			probers: make(map[string]Prober),
		}

		_, err := pm.getOrCreateProber("invalid")
		if err == nil {
			t.Error("Expected error for invalid probe type")
		}
	})
}

func TestTransformTargetEdgeCases(t *testing.T) {
	config := map[string]*ProberConfig{
		"http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"custom": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
	}

	pm := &probeManager{
		config:      config,
		defaultType: "http",
	}

	tests := []struct {
		name           string
		input          string
		expectedTarget string
		expectedProber string
	}{
		{
			name:           "Plain hostname",
			input:          "example.com",
			expectedTarget: "http://example.com",
			expectedProber: "http",
		},
		{
			name:           "IPv4 address",
			input:          "192.168.1.1",
			expectedTarget: "http://192.168.1.1",
			expectedProber: "http",
		},
		{
			name:           "URL with port",
			input:          "custom://example.com:8080/path",
			expectedTarget: "custom://example.com:8080/path",
			expectedProber: "custom",
		},
		{
			name:           "Complex legacy format",
			input:          "custom:complex.example.com:443",
			expectedTarget: "custom://complex.example.com:443",
			expectedProber: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, prober := pm.transformTarget(tt.input)

			if target != tt.expectedTarget {
				t.Errorf("Expected target: %s, got: %s", tt.expectedTarget, target)
			}

			if prober != tt.expectedProber {
				t.Errorf("Expected prober: %s, got: %s", tt.expectedProber, prober)
			}
		})
	}
}
