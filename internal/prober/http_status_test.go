package prober

import (
	"testing"
)

func TestHTTPStatusCodeMatching(t *testing.T) {
	tests := []struct {
		name       string
		config     *HTTPConfig
		statusCode int
		expected   bool
	}{
		// Backward compatibility tests
		{
			name: "exact match - success",
			config: &HTTPConfig{
				ExpectCode: 200,
			},
			statusCode: 200,
			expected:   true,
		},
		{
			name: "exact match - failure",
			config: &HTTPConfig{
				ExpectCode: 200,
			},
			statusCode: 404,
			expected:   false,
		},
		{
			name: "no expectation - any code accepted",
			config: &HTTPConfig{
				ExpectCode: 0,
			},
			statusCode: 500,
			expected:   true,
		},

		// Range matching tests
		{
			name: "2XX range - 200",
			config: &HTTPConfig{
				ExpectCodes: "2XX",
			},
			statusCode: 200,
			expected:   true,
		},
		{
			name: "2XX range - 201",
			config: &HTTPConfig{
				ExpectCodes: "2XX",
			},
			statusCode: 201,
			expected:   true,
		},
		{
			name: "2XX range - 299",
			config: &HTTPConfig{
				ExpectCodes: "2XX",
			},
			statusCode: 299,
			expected:   true,
		},
		{
			name: "2XX range - 300 failure",
			config: &HTTPConfig{
				ExpectCodes: "2XX",
			},
			statusCode: 300,
			expected:   false,
		},
		{
			name: "3XX range - 301",
			config: &HTTPConfig{
				ExpectCodes: "3XX",
			},
			statusCode: 301,
			expected:   true,
		},
		{
			name: "4XX range - 404",
			config: &HTTPConfig{
				ExpectCodes: "4XX",
			},
			statusCode: 404,
			expected:   true,
		},
		{
			name: "5XX range - 500",
			config: &HTTPConfig{
				ExpectCodes: "5XX",
			},
			statusCode: 500,
			expected:   true,
		},

		// List matching tests
		{
			name: "list - 200,201,202 - 200 match",
			config: &HTTPConfig{
				ExpectCodes: "200,201,202",
			},
			statusCode: 200,
			expected:   true,
		},
		{
			name: "list - 200,201,202 - 201 match",
			config: &HTTPConfig{
				ExpectCodes: "200,201,202",
			},
			statusCode: 201,
			expected:   true,
		},
		{
			name: "list - 200,201,202 - 204 no match",
			config: &HTTPConfig{
				ExpectCodes: "200,201,202",
			},
			statusCode: 204,
			expected:   false,
		},
		{
			name: "list with redirects - 200,301,302",
			config: &HTTPConfig{
				ExpectCodes: "200,301,302",
			},
			statusCode: 301,
			expected:   true,
		},

		// Single code as string
		{
			name: "string single code - 200",
			config: &HTTPConfig{
				ExpectCodes: "200",
			},
			statusCode: 200,
			expected:   true,
		},
		{
			name: "string single code - 404 failure",
			config: &HTTPConfig{
				ExpectCodes: "200",
			},
			statusCode: 404,
			expected:   false,
		},

		// Edge cases
		{
			name: "invalid range - 6XX",
			config: &HTTPConfig{
				ExpectCodes: "6XX",
			},
			statusCode: 600,
			expected:   false,
		},
		{
			name: "invalid format",
			config: &HTTPConfig{
				ExpectCodes: "invalid",
			},
			statusCode: 200,
			expected:   false,
		},
		{
			name: "empty expect_codes with expect_code fallback",
			config: &HTTPConfig{
				ExpectCode:  200,
				ExpectCodes: "",
			},
			statusCode: 200,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prober := NewHTTPProber(tt.config, "test")
			result := prober.isExpectedStatusCode(tt.statusCode)
			
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for status code %d with config %+v", 
					tt.expected, result, tt.statusCode, tt.config)
			}
		})
	}
}

func TestHTTPStatusCodePatternMatching(t *testing.T) {
	prober := NewHTTPProber(&HTTPConfig{}, "test")
	
	tests := []struct {
		name       string
		pattern    string
		statusCode int
		expected   bool
	}{
		// Range patterns
		{"1XX - 100", "1XX", 100, true},
		{"1XX - 199", "1XX", 199, true},
		{"1XX - 200", "1XX", 200, false},
		{"2XX - 200", "2XX", 200, true},
		{"2XX - 299", "2XX", 299, true},
		{"2XX - 300", "2XX", 300, false},
		{"3XX - 301", "3XX", 301, true},
		{"4XX - 404", "4XX", 404, true},
		{"5XX - 500", "5XX", 500, true},
		
		// Comma-separated lists
		{"200,201 - 200", "200,201", 200, true},
		{"200,201 - 201", "200,201", 201, true},
		{"200,201 - 202", "200,201", 202, false},
		{"200, 301, 302 - 301", "200, 301, 302", 301, true},
		
		// Single codes
		{"200 - 200", "200", 200, true},
		{"200 - 404", "200", 404, false},
		
		// Edge cases
		{"empty pattern", "", 200, false},
		{"invalid pattern", "invalid", 200, false},
		{"mixed invalid", "200,invalid,301", 301, true},
		{"mixed invalid", "200,invalid,301", 999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.matchStatusCodePattern(tt.statusCode, tt.pattern)
			
			if result != tt.expected {
				t.Errorf("Pattern %q with status %d: expected %v, got %v", 
					tt.pattern, tt.statusCode, tt.expected, result)
			}
		})
	}
}