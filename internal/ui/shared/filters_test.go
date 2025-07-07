package shared

import (
	"testing"
	"time"

	"github.com/servak/mping/internal/stats"
)

func TestFilterMetrics(t *testing.T) {
	// Create test metrics
	metrics := []stats.Metrics{
		{
			Name:         "google.com",
			Total:        100,
			Successful:   95,
			Failed:       5,
			LastSuccTime: time.Now(),
		},
		{
			Name:         "yahoo.com",
			Total:        50,
			Successful:   48,
			Failed:       2,
			LastSuccTime: time.Now(),
		},
		{
			Name:         "example.org",
			Total:        25,
			Successful:   25,
			Failed:       0,
			LastSuccTime: time.Now(),
		},
		{
			Name:         "test.net",
			Total:        10,
			Successful:   8,
			Failed:       2,
			LastSuccTime: time.Now(),
		},
	}

	tests := []struct {
		name           string
		filterText     string
		expectedCount  int
		expectedNames  []string
	}{
		{
			name:          "empty filter returns all metrics",
			filterText:    "",
			expectedCount: 4,
			expectedNames: []string{"google.com", "yahoo.com", "example.org", "test.net"},
		},
		{
			name:          "filter by partial domain name",
			filterText:    "goo",
			expectedCount: 1,
			expectedNames: []string{"google.com"},
		},
		{
			name:          "filter by TLD",
			filterText:    ".com",
			expectedCount: 2,
			expectedNames: []string{"google.com", "yahoo.com"},
		},
		{
			name:          "case insensitive filtering",
			filterText:    "GOOGLE",
			expectedCount: 1,
			expectedNames: []string{"google.com"},
		},
		{
			name:          "filter matches multiple hosts",
			filterText:    "e",
			expectedCount: 3,
			expectedNames: []string{"google.com", "example.org", "test.net"},
		},
		{
			name:          "filter matches no hosts",
			filterText:    "nonexistent",
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:          "filter by exact match",
			filterText:    "test.net",
			expectedCount: 1,
			expectedNames: []string{"test.net"},
		},
		{
			name:          "filter with mixed case",
			filterText:    "YaHoO",
			expectedCount: 1,
			expectedNames: []string{"yahoo.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterMetrics(metrics, tt.filterText)

			// Check count
			if len(result) != tt.expectedCount {
				t.Errorf("FilterMetrics() returned %d metrics, want %d", len(result), tt.expectedCount)
			}

			// Check that all expected names are present
			resultNames := make(map[string]bool)
			for _, metric := range result {
				resultNames[metric.Name] = true
			}

			for _, expectedName := range tt.expectedNames {
				if !resultNames[expectedName] {
					t.Errorf("FilterMetrics() missing expected metric: %s", expectedName)
				}
			}

			// Check that no unexpected names are present
			if len(resultNames) != len(tt.expectedNames) {
				actualNames := make([]string, 0, len(result))
				for _, metric := range result {
					actualNames = append(actualNames, metric.Name)
				}
				t.Errorf("FilterMetrics() returned unexpected metrics. Got: %v, Want: %v", actualNames, tt.expectedNames)
			}
		})
	}
}

func TestFilterMetricsPreservesOrder(t *testing.T) {
	metrics := []stats.Metrics{
		{Name: "alpha.com"},
		{Name: "beta.com"},
		{Name: "gamma.com"},
	}

	result := FilterMetrics(metrics, ".com")

	expectedOrder := []string{"alpha.com", "beta.com", "gamma.com"}
	for i, metric := range result {
		if metric.Name != expectedOrder[i] {
			t.Errorf("FilterMetrics() changed order. Got %s at position %d, want %s", metric.Name, i, expectedOrder[i])
		}
	}
}

func TestFilterMetricsWithEmptyMetrics(t *testing.T) {
	var metrics []stats.Metrics

	result := FilterMetrics(metrics, "test")

	if len(result) != 0 {
		t.Errorf("FilterMetrics() with empty metrics returned %d items, want 0", len(result))
	}
}

func TestFilterMetricsDoesNotModifyOriginal(t *testing.T) {
	original := []stats.Metrics{
		{Name: "test1.com"},
		{Name: "test2.com"},
		{Name: "example.org"},
	}

	// Create a copy to compare later
	originalCopy := make([]stats.Metrics, len(original))
	copy(originalCopy, original)

	// Filter metrics
	FilterMetrics(original, "test")

	// Verify original slice is unchanged
	if len(original) != len(originalCopy) {
		t.Error("FilterMetrics() modified the original slice length")
	}

	for i, metric := range original {
		if metric.Name != originalCopy[i].Name {
			t.Errorf("FilterMetrics() modified original slice at index %d: got %s, want %s", i, metric.Name, originalCopy[i].Name)
		}
	}
}