package prober

import (
	"testing"
)

func TestResolveSourceInterface(t *testing.T) {
	tests := []struct {
		name           string
		sourceInterface string
		probeType      ProbeType
		expectError    bool
		expectedAddr   string
	}{
		{
			name:           "Empty interface returns default IPv4",
			sourceInterface: "",
			probeType:      ICMPV4,
			expectError:    false,
			expectedAddr:   "0.0.0.0",
		},
		{
			name:           "Empty interface returns default IPv6",
			sourceInterface: "",
			probeType:      ICMPV6,
			expectError:    false,
			expectedAddr:   "::",
		},
		{
			name:           "Valid IPv4 address",
			sourceInterface: "127.0.0.1",
			probeType:      ICMPV4,
			expectError:    false,
			expectedAddr:   "127.0.0.1",
		},
		{
			name:           "Valid IPv6 address",
			sourceInterface: "::1",
			probeType:      ICMPV6,
			expectError:    false,
			expectedAddr:   "::1",
		},
		{
			name:           "IPv6 address for IPv4 probe should fail",
			sourceInterface: "::1",
			probeType:      ICMPV4,
			expectError:    true,
		},
		{
			name:           "IPv4 address for IPv6 probe should fail",
			sourceInterface: "127.0.0.1",
			probeType:      ICMPV6,
			expectError:    true,
		},
		{
			name:           "Invalid interface name",
			sourceInterface: "nonexistent-interface",
			probeType:      ICMPV4,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := resolveSourceInterface(tt.sourceInterface, tt.probeType)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			
			if addr != tt.expectedAddr {
				t.Errorf("Expected address %s, got %s", tt.expectedAddr, addr)
			}
		})
	}
}

func TestResolveSourceInterfaceLoopback(t *testing.T) {
	// Test with loopback interface (should exist on most systems)
	addr, err := resolveSourceInterface("lo", ICMPV4)
	if err != nil {
		// Skip if loopback interface doesn't exist or has no IPv4 address
		t.Skipf("Loopback interface test skipped: %v", err)
		return
	}
	
	if addr != "127.0.0.1" {
		t.Logf("Loopback IPv4 address: %s (expected 127.0.0.1, but this may vary)", addr)
	}
}