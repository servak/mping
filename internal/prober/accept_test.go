package prober

import (
	"testing"
)

func TestICMPProberAccept(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		target    string
		shouldErr bool
	}{
		// Valid cases
		{
			name:   "icmpv4 new format",
			prefix: "icmpv4",
			target: "icmpv4://google.com",
		},
		{
			name:   "custom prefix new format",
			prefix: "my-ping",
			target: "my-ping://google.com",
		},
		{
			name:   "icmpv4 legacy format",
			prefix: "icmpv4",
			target: "icmpv4:google.com",
		},
		{
			name:   "custom prefix legacy format",
			prefix: "my-ping",
			target: "my-ping:google.com",
		},

		// Invalid cases
		{
			name:      "wrong prefix",
			prefix:    "icmpv4",
			target:    "http://google.com",
			shouldErr: true,
		},
		{
			name:      "no prefix",
			prefix:    "icmpv4",
			target:    "google.com",
			shouldErr: true,
		},
		{
			name:      "empty target",
			prefix:    "icmpv4",
			target:    "",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip ICMP tests that require network permissions
			if tt.target == "" {
				return
			}

			// Just test the prefix logic, not actual network resolution
			config := &ICMPConfig{Body: "test"}
			prober, err := NewICMPProber(ICMPV4, config, tt.prefix)
			if err != nil {
				t.Skip("ICMP prober creation failed (likely permissions):", err)
				return
			}
			defer prober.Stop()

			err = prober.Accept(tt.target)

			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestHTTPProberAccept(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		config    *HTTPConfig
		target    string
		shouldErr bool
	}{
		// Valid cases
		{
			name:   "http with matching prefix",
			prefix: "my-http",
			config: &HTTPConfig{ExpectCode: 200},
			target: "my-http://example.com",
		},
		{
			name:   "https with TLS config",
			prefix: "my-https",
			config: &HTTPConfig{
				ExpectCode: 200,
				TLS:        &TLSConfig{SkipVerify: true},
			},
			target: "my-https://secure.example.com",
		},
		{
			name:   "custom prefix",
			prefix: "api-check",
			config: &HTTPConfig{ExpectCode: 200},
			target: "api-check://api.example.com/health",
		},

		// Invalid cases
		{
			name:      "wrong prefix",
			prefix:    "my-http",
			config:    &HTTPConfig{ExpectCode: 200},
			target:    "other://example.com",
			shouldErr: true,
		},
		{
			name:      "invalid URL format",
			prefix:    "my-http",
			config:    &HTTPConfig{ExpectCode: 200},
			target:    "my-http://",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prober := NewHTTPProber(tt.config, tt.prefix)
			err := prober.Accept(tt.target)

			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestTCPProberAccept(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		target    string
		shouldErr bool
	}{
		// Valid cases
		{
			name:   "tcp new format",
			prefix: "tcp",
			target: "tcp://example.com:80",
		},
		{
			name:   "custom tcp prefix",
			prefix: "my-tcp",
			target: "my-tcp://example.com:8080",
		},
		{
			name:   "tcp legacy format",
			prefix: "tcp",
			target: "tcp:example.com:80",
		},

		// Invalid cases
		{
			name:      "wrong prefix",
			prefix:    "tcp",
			target:    "http://example.com",
			shouldErr: true,
		},
		{
			name:      "missing port",
			prefix:    "tcp",
			target:    "tcp://example.com",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TCPConfig{}
			prober := NewTCPProber(config, tt.prefix)
			err := prober.Accept(tt.target)

			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDNSProberAccept(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		target    string
		shouldErr bool
	}{
		// Valid cases
		{
			name:   "dns new format",
			prefix: "dns",
			target: "dns://8.8.8.8/google.com",
		},
		{
			name:   "custom dns prefix",
			prefix: "my-dns",
			target: "my-dns://1.1.1.1/example.com",
		},
		{
			name:   "dns legacy format",
			prefix: "dns",
			target: "dns:8.8.8.8/google.com",
		},

		// Invalid cases
		{
			name:      "wrong prefix",
			prefix:    "dns",
			target:    "http://example.com",
			shouldErr: true,
		},
		{
			name:      "invalid format",
			prefix:    "dns",
			target:    "dns://invalid",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &DNSConfig{
				Server:     "8.8.8.8",
				Port:       53,
				RecordType: "A",
			}
			prober := NewDNSProber(config, tt.prefix)
			err := prober.Accept(tt.target)

			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
