package prober

import (
	"strings"
	"testing"
)

func TestICMPConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *ICMPConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &ICMPConfig{
				Body: "test",
				TOS:  0,
				TTL:  64,
			},
			wantErr: false,
		},
		{
			name: "valid config with max values",
			config: &ICMPConfig{
				Body: "test",
				TOS:  255,
				TTL:  255,
			},
			wantErr: false,
		},
		{
			name: "invalid TOS - negative",
			config: &ICMPConfig{
				Body: "test",
				TOS:  -1,
				TTL:  64,
			},
			wantErr: true,
			errMsg:  "invalid TOS value",
		},
		{
			name: "invalid TOS - too large",
			config: &ICMPConfig{
				Body: "test",
				TOS:  256,
				TTL:  64,
			},
			wantErr: true,
			errMsg:  "invalid TOS value",
		},
		{
			name: "invalid TTL - negative",
			config: &ICMPConfig{
				Body: "test",
				TOS:  0,
				TTL:  -1,
			},
			wantErr: true,
			errMsg:  "invalid TTL value",
		},
		{
			name: "invalid TTL - too large",
			config: &ICMPConfig{
				Body: "test",
				TOS:  0,
				TTL:  256,
			},
			wantErr: true,
			errMsg:  "invalid TTL value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

func TestHTTPConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *HTTPConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config - empty expect codes",
			config: &HTTPConfig{
				ExpectCodes: "",
			},
			wantErr: false,
		},
		{
			name: "valid config - single code",
			config: &HTTPConfig{
				ExpectCodes: "200",
			},
			wantErr: false,
		},
		{
			name: "valid config - range",
			config: &HTTPConfig{
				ExpectCodes: "200-299",
			},
			wantErr: false,
		},
		{
			name: "valid config - list",
			config: &HTTPConfig{
				ExpectCodes: "200,201,202",
			},
			wantErr: false,
		},
		{
			name: "invalid config - bad pattern",
			config: &HTTPConfig{
				ExpectCodes: "200-",
			},
			wantErr: true,
			errMsg:  "invalid expect_codes pattern",
		},
		{
			name: "invalid config - bad range",
			config: &HTTPConfig{
				ExpectCodes: "200-abc",
			},
			wantErr: true,
			errMsg:  "invalid expect_codes pattern",
		},
		{
			name: "invalid config - invalid characters",
			config: &HTTPConfig{
				ExpectCodes: "200@300",
			},
			wantErr: true,
			errMsg:  "invalid expect_codes pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

func TestTCPConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config *TCPConfig
	}{
		{
			name: "valid config - empty",
			config: &TCPConfig{},
		},
		{
			name: "valid config - with source interface",
			config: &TCPConfig{
				SourceInterface: "eth0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err != nil {
				t.Errorf("TCP config validation should not fail: %v", err)
			}
		})
	}
}

func TestDNSConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *DNSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       53,
				RecordType: "A",
			},
			wantErr: false,
		},
		{
			name: "valid config - different record types",
			config: &DNSConfig{
				Server:     "1.1.1.1",
				Port:       53,
				RecordType: "AAAA",
			},
			wantErr: false,
		},
		{
			name: "valid config - with expect codes",
			config: &DNSConfig{
				Server:      "8.8.8.8",
				Port:        53,
				RecordType:  "A",
				ExpectCodes: "0,2,3",
			},
			wantErr: false,
		},
		{
			name: "invalid config - empty server",
			config: &DNSConfig{
				Server:     "",
				Port:       53,
				RecordType: "A",
			},
			wantErr: true,
			errMsg:  "DNS server is required",
		},
		{
			name: "invalid config - invalid port 0",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       0,
				RecordType: "A",
			},
			wantErr: true,
			errMsg:  "invalid DNS server port",
		},
		{
			name: "invalid config - invalid port negative",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       -1,
				RecordType: "A",
			},
			wantErr: true,
			errMsg:  "invalid DNS server port",
		},
		{
			name: "invalid config - invalid port too large",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       65536,
				RecordType: "A",
			},
			wantErr: true,
			errMsg:  "invalid DNS server port",
		},
		{
			name: "invalid config - invalid record type",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       53,
				RecordType: "INVALID",
			},
			wantErr: true,
			errMsg:  "invalid DNS record type",
		},
		{
			name: "invalid config - invalid expect codes",
			config: &DNSConfig{
				Server:      "8.8.8.8",
				Port:        53,
				RecordType:  "A",
				ExpectCodes: "0-",
			},
			wantErr: true,
			errMsg:  "invalid expect_codes pattern",
		},
		{
			name: "valid config - case insensitive record type",
			config: &DNSConfig{
				Server:     "8.8.8.8",
				Port:       53,
				RecordType: "a", // lowercase should be valid
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

func TestProberConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *ProberConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ICMP config",
			config: &ProberConfig{
				Probe: ICMPV4,
				ICMP: &ICMPConfig{
					Body: "test",
				},
			},
			wantErr: false,
		},
		{
			name: "valid HTTP config",
			config: &ProberConfig{
				Probe: HTTP,
				HTTP: &HTTPConfig{
					ExpectCodes: "200",
				},
			},
			wantErr: false,
		},
		{
			name: "valid TCP config",
			config: &ProberConfig{
				Probe: TCP,
				TCP:   &TCPConfig{},
			},
			wantErr: false,
		},
		{
			name: "valid DNS config",
			config: &ProberConfig{
				Probe: DNS,
				DNS: &DNSConfig{
					Server:     "8.8.8.8",
					Port:       53,
					RecordType: "A",
				},
			},
			wantErr: false,
		},
		{
			name: "ICMP config missing",
			config: &ProberConfig{
				Probe: ICMPV4,
				ICMP:  nil,
			},
			wantErr: true,
			errMsg:  "ICMP config required",
		},
		{
			name: "HTTP config missing",
			config: &ProberConfig{
				Probe: HTTP,
				HTTP:  nil,
			},
			wantErr: true,
			errMsg:  "HTTP config required",
		},
		{
			name: "TCP config missing",
			config: &ProberConfig{
				Probe: TCP,
				TCP:   nil,
			},
			wantErr: true,
			errMsg:  "TCP config required",
		},
		{
			name: "DNS config missing",
			config: &ProberConfig{
				Probe: DNS,
				DNS:   nil,
			},
			wantErr: true,
			errMsg:  "DNS config required",
		},
		{
			name: "unknown probe type",
			config: &ProberConfig{
				Probe: "unknown",
			},
			wantErr: true,
			errMsg:  "unknown probe type",
		},
		{
			name: "invalid ICMP config",
			config: &ProberConfig{
				Probe: ICMPV4,
				ICMP: &ICMPConfig{
					Body: "test",
					TOS:  999, // Invalid TOS
				},
			},
			wantErr: true,
			errMsg:  "invalid TOS value",
		},
		{
			name: "invalid HTTP config",
			config: &ProberConfig{
				Probe: HTTP,
				HTTP: &HTTPConfig{
					ExpectCodes: "200-", // Invalid pattern
				},
			},
			wantErr: true,
			errMsg:  "invalid expect_codes pattern",
		},
		{
			name: "invalid DNS config",
			config: &ProberConfig{
				Probe: DNS,
				DNS: &DNSConfig{
					Server:     "", // Empty server
					Port:       53,
					RecordType: "A",
				},
			},
			wantErr: true,
			errMsg:  "DNS server is required",
		},
		{
			name: "valid NTP config",
			config: &ProberConfig{
				Probe: NTP,
				NTP: &NTPConfig{
					Server: "pool.ntp.org",
					Port:   123,
				},
			},
			wantErr: false,
		},
		{
			name: "NTP config missing",
			config: &ProberConfig{
				Probe: NTP,
				NTP:   nil,
			},
			wantErr: true,
			errMsg:  "NTP config required",
		},
		{
			name: "invalid NTP config",
			config: &ProberConfig{
				Probe: NTP,
				NTP: &NTPConfig{
					Server: "", // Empty server
					Port:   123,
				},
			},
			wantErr: true,
			errMsg:  "NTP server is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}