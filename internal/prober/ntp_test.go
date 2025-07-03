package prober

import (
	"strings"
	"testing"
	"time"
)

func TestNTPConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *NTPConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   123,
			},
			wantErr: false,
		},
		{
			name: "valid config with max offset",
			config: &NTPConfig{
				Server:    "time.google.com",
				Port:      123,
				MaxOffset: 1000 * time.Millisecond,
			},
			wantErr: false,
		},
		{
			name: "invalid config - empty server",
			config: &NTPConfig{
				Server: "",
				Port:   123,
			},
			wantErr: true,
			errMsg:  "NTP server is required",
		},
		{
			name: "invalid config - invalid port 0",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   0,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
		},
		{
			name: "invalid config - invalid port negative",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   -1,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
		},
		{
			name: "invalid config - invalid port too large",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   65536,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
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

func TestNTPProberAccept(t *testing.T) {
	config := &NTPConfig{
		Server: "pool.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	tests := []struct {
		name    string
		target  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid new format",
			target:  "ntp://time.google.com",
			wantErr: false,
		},
		{
			name:    "valid new format with port",
			target:  "ntp://time.google.com:123",
			wantErr: false,
		},
		{
			name:    "valid legacy format",
			target:  "ntp:time.google.com",
			wantErr: false,
		},
		{
			name:    "valid legacy format with port",
			target:  "ntp:time.google.com:123",
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			target:  "http://time.google.com",
			wantErr: true,
			errMsg:  "not accepted",
		},
		{
			name:    "invalid port format",
			target:  "ntp://time.google.com:abc",
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name:    "invalid server:port format",
			target:  "ntp://[::1",
			wantErr: true,
			errMsg:  "invalid server:port format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := prober.Accept(tt.target)
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestNTPProberParseTarget(t *testing.T) {
	config := &NTPConfig{
		Server: "default.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	tests := []struct {
		name       string
		target     string
		wantServer string
		wantPort   int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "new format - server only",
			target:     "ntp://time.google.com",
			wantServer: "time.google.com",
			wantPort:   123, // default port
			wantErr:    false,
		},
		{
			name:       "new format - server with port",
			target:     "ntp://time.google.com:1123",
			wantServer: "time.google.com",
			wantPort:   1123,
			wantErr:    false,
		},
		{
			name:       "legacy format - server only",
			target:     "ntp:time.google.com",
			wantServer: "time.google.com",
			wantPort:   123, // default port
			wantErr:    false,
		},
		{
			name:       "legacy format - server with port",
			target:     "ntp:time.google.com:1123",
			wantServer: "time.google.com",
			wantPort:   1123,
			wantErr:    false,
		},
		{
			name:       "empty target - use config defaults",
			target:     "ntp://",
			wantServer: "default.ntp.org",
			wantPort:   123,
			wantErr:    false,
		},
		{
			name:    "invalid port",
			target:  "ntp://time.google.com:abc",
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name:    "invalid server:port format",
			target:  "ntp://[::1",
			wantErr: true,
			errMsg:  "invalid server:port format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, port, err := prober.parseTarget(tt.target)
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if server != tt.wantServer {
					t.Errorf("Expected server '%s', got '%s'", tt.wantServer, server)
				}
				if port != tt.wantPort {
					t.Errorf("Expected port %d, got %d", tt.wantPort, port)
				}
			}
		})
	}
}

func TestNTPTimeConversion(t *testing.T) {
	// Test time conversion functions
	now := time.Now()
	
	// Convert to NTP format and back
	sec, frac := ntpTimeFromTime(now)
	converted := ntpTimeToTime(sec, frac)
	
	// Allow small difference due to precision loss
	diff := converted.Sub(now).Abs()
	if diff > time.Microsecond {
		t.Errorf("Time conversion precision loss too large: %v", diff)
	}
}

func TestNTPProberRegistration(t *testing.T) {
	config := &NTPConfig{
		Server: "pool.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	// Add some targets
	targets := []string{
		"ntp://time.google.com",
		"ntp://pool.ntp.org:123",
	}

	for _, target := range targets {
		err := prober.Accept(target)
		if err != nil {
			t.Errorf("Failed to accept target %s: %v", target, err)
		}
	}

	// Test registration events
	events := make(chan *Event, 10)
	prober.emitRegistrationEvents(events)
	close(events)

	// Count registration events
	count := 0
	for event := range events {
		if event.Result != REGISTER {
			t.Errorf("Expected REGISTER event, got %v", event.Result)
		}
		count++
	}

	if count != len(targets) {
		t.Errorf("Expected %d registration events, got %d", len(targets), count)
	}
}