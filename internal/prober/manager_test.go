package prober

import (
	"strings"
	"testing"
)

func TestTransformTarget(t *testing.T) {
	// Mock config for testing
	config := map[string]*ProberConfig{
		"icmpv4": {
			Probe: ICMPV4,
			ICMP:  &ICMPConfig{Body: "test"},
		},
		"icmpv6": {
			Probe: ICMPV6,
			ICMP:  &ICMPConfig{Body: "test"},
		},
		"http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"https": {
			Probe: HTTPS,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"tcp": {
			Probe: TCP,
			TCP:   &TCPConfig{},
		},
		"dns": {
			Probe: DNS,
			DNS:   &DNSConfig{Server: "8.8.8.8", Port: 53, RecordType: "A"},
		},
		"my-ping": {
			Probe: ICMPV4,
			ICMP:  &ICMPConfig{Body: "custom"},
		},
	}

	tests := []struct {
		name           string
		input          string
		defaultType    string
		expectedTarget string
		expectedProber string
		shouldFail     bool
	}{
		// New unified format (name://target)
		{
			name:           "icmpv4 new format",
			input:          "icmpv4://google.com",
			defaultType:    "icmpv4",
			expectedTarget: "icmpv4://google.com",
			expectedProber: "icmpv4",
		},
		{
			name:           "custom prober name",
			input:          "my-ping://google.com",
			defaultType:    "icmpv4",
			expectedTarget: "my-ping://google.com",
			expectedProber: "my-ping",
		},
		{
			name:           "http new format",
			input:          "http://google.com",
			defaultType:    "icmpv4",
			expectedTarget: "http://google.com",
			expectedProber: "http",
		},
		{
			name:           "https new format",
			input:          "https://google.com",
			defaultType:    "icmpv4",
			expectedTarget: "https://google.com",
			expectedProber: "https",
		},

		// Legacy format conversion (name:target -> name://target)
		{
			name:           "icmpv4 legacy format conversion",
			input:          "icmpv4:google.com",
			defaultType:    "icmpv4",
			expectedTarget: "icmpv4://google.com",
			expectedProber: "icmpv4",
		},
		{
			name:           "icmpv6 legacy format conversion",
			input:          "icmpv6:google.com",
			defaultType:    "icmpv4",
			expectedTarget: "icmpv6://google.com",
			expectedProber: "icmpv6",
		},
		{
			name:           "tcp legacy format conversion",
			input:          "tcp:google.com:80",
			defaultType:    "icmpv4",
			expectedTarget: "tcp://google.com:80",
			expectedProber: "tcp",
		},
		{
			name:           "dns legacy format conversion",
			input:          "dns:8.8.8.8/google.com",
			defaultType:    "icmpv4",
			expectedTarget: "dns://8.8.8.8/google.com",
			expectedProber: "dns",
		},

		// Default handling
		{
			name:           "plain hostname with default",
			input:          "google.com",
			defaultType:    "icmpv4",
			expectedTarget: "icmpv4://google.com",
			expectedProber: "icmpv4",
		},
		{
			name:           "plain hostname with custom default",
			input:          "google.com",
			defaultType:    "my-ping",
			expectedTarget: "my-ping://google.com",
			expectedProber: "my-ping",
		},

		// Error cases
		{
			name:        "unknown prefix",
			input:       "unknown://google.com",
			defaultType: "icmpv4",
			shouldFail:  true,
		},
		{
			name:        "no default configured",
			input:       "google.com",
			defaultType: "",
			shouldFail:  true,
		},
		{
			name:        "invalid default type",
			input:       "google.com",
			defaultType: "nonexistent",
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := &probeManager{
				config:      config,
				defaultType: tt.defaultType,
			}

			target, proberType := pm.transformTarget(tt.input)

			if tt.shouldFail {
				if proberType != "" {
					t.Errorf("Expected failure, but got proberType: %s", proberType)
				}
				return
			}

			if target != tt.expectedTarget {
				t.Errorf("Expected target: %s, got: %s", tt.expectedTarget, target)
			}

			if proberType != tt.expectedProber {
				t.Errorf("Expected prober: %s, got: %s", tt.expectedProber, proberType)
			}
		})
	}
}

func TestProbeManagerIntegration(t *testing.T) {
	// Use HTTP probers for integration tests (no special permissions required)
	config := map[string]*ProberConfig{
		"http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200},
		},
		"my-http": {
			Probe: HTTP,
			HTTP:  &HTTPConfig{ExpectCode: 200, ExpectBody: "custom"},
		},
		"my-https": {
			Probe: HTTP,
			HTTP: &HTTPConfig{
				ExpectCode: 200,
				TLS:        &TLSConfig{SkipVerify: true},
			},
		},
	}

	tests := []struct {
		name      string
		targets   []string
		shouldErr bool
	}{
		{
			name:    "mixed target formats",
			targets: []string{"google.com", "http://google.com", "my-http://example.com", "my-https://secure.example.com"},
		},
		{
			name:      "unknown prober",
			targets:   []string{"unknown://google.com"},
			shouldErr: true,
		},
		{
			name:      "no default and plain hostname",
			targets:   []string{"google.com"},
			shouldErr: false, // Should use default "http"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh ProbeManager for each test
			testPM := NewProbeManager(config, "http")
			err := testPM.AddTargets(tt.targets...)

			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestHTTPProberTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *HTTPConfig
		target      string
		expectedURL string
	}{
		{
			name: "HTTP without TLS",
			config: &HTTPConfig{
				ExpectCode: 200,
			},
			target:      "my-http://example.com",
			expectedURL: "http://example.com",
		},
		{
			name: "HTTPS with TLS config",
			config: &HTTPConfig{
				ExpectCode: 200,
				TLS:        &TLSConfig{SkipVerify: true},
			},
			target:      "my-https://secure.example.com",
			expectedURL: "https://secure.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract prefix from target for proper testing
			prefix := strings.SplitN(tt.target, "://", 2)[0]
			prober := NewHTTPProber(tt.config, prefix)
			actualURL := prober.convertToActualURL(tt.target)

			if actualURL != tt.expectedURL {
				t.Errorf("Expected URL: %s, got: %s", tt.expectedURL, actualURL)
			}
		})
	}
}
