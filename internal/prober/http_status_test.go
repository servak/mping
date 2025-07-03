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
			name: "200-299 range - 200",
			config: &HTTPConfig{
				ExpectCodes: "200-299",
			},
			statusCode: 200,
			expected:   true,
		},
		{
			name: "200-299 range - 250",
			config: &HTTPConfig{
				ExpectCodes: "200-299",
			},
			statusCode: 250,
			expected:   true,
		},
		{
			name: "200-299 range - 299",
			config: &HTTPConfig{
				ExpectCodes: "200-299",
			},
			statusCode: 299,
			expected:   true,
		},
		{
			name: "200-299 range - 300 failure",
			config: &HTTPConfig{
				ExpectCodes: "200-299",
			},
			statusCode: 300,
			expected:   false,
		},
		{
			name: "300-399 range - 301",
			config: &HTTPConfig{
				ExpectCodes: "300-399",
			},
			statusCode: 301,
			expected:   true,
		},
		{
			name: "400-499 range - 404",
			config: &HTTPConfig{
				ExpectCodes: "400-499",
			},
			statusCode: 404,
			expected:   true,
		},
		{
			name: "500-599 range - 500",
			config: &HTTPConfig{
				ExpectCodes: "500-599",
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
			name: "mixed patterns",
			config: &HTTPConfig{
				ExpectCodes: "200,300-399",
			},
			statusCode: 350,
			expected:   true,
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

