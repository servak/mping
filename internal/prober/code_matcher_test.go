package prober

import (
	"testing"
)

func TestCodeMatcher(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		code     int
		expected bool
	}{
		// Single code tests
		{
			name:     "exact match",
			pattern:  "200",
			code:     200,
			expected: true,
		},
		{
			name:     "exact no match",
			pattern:  "200",
			code:     404,
			expected: false,
		},

		// Range tests
		{
			name:     "range match - start",
			pattern:  "200-299",
			code:     200,
			expected: true,
		},
		{
			name:     "range match - middle",
			pattern:  "200-299",
			code:     250,
			expected: true,
		},
		{
			name:     "range match - end",
			pattern:  "200-299",
			code:     299,
			expected: true,
		},
		{
			name:     "range no match - below",
			pattern:  "200-299",
			code:     199,
			expected: false,
		},
		{
			name:     "range no match - above",
			pattern:  "200-299",
			code:     300,
			expected: false,
		},

		// Comma-separated list tests
		{
			name:     "list match - first",
			pattern:  "200,201,202",
			code:     200,
			expected: true,
		},
		{
			name:     "list match - middle",
			pattern:  "200,201,202",
			code:     201,
			expected: true,
		},
		{
			name:     "list match - last",
			pattern:  "200,201,202",
			code:     202,
			expected: true,
		},
		{
			name:     "list no match",
			pattern:  "200,201,202",
			code:     204,
			expected: false,
		},

		// Mixed patterns
		{
			name:     "mixed - single and range - match single",
			pattern:  "200,300-399",
			code:     200,
			expected: true,
		},
		{
			name:     "mixed - single and range - match range",
			pattern:  "200,300-399",
			code:     350,
			expected: true,
		},
		{
			name:     "mixed - single and range - no match",
			pattern:  "200,300-399",
			code:     250,
			expected: false,
		},
		{
			name:     "mixed - multiple ranges",
			pattern:  "100-199,300-399",
			code:     150,
			expected: true,
		},
		{
			name:     "mixed - multiple ranges - second range",
			pattern:  "100-199,300-399",
			code:     350,
			expected: true,
		},
		{
			name:     "mixed - multiple ranges - no match",
			pattern:  "100-199,300-399",
			code:     250,
			expected: false,
		},

		// Special cases
		{
			name:     "empty pattern matches any",
			pattern:  "",
			code:     500,
			expected: true,
		},
		{
			name:     "whitespace handling",
			pattern:  " 200 , 201 , 202 ",
			code:     201,
			expected: true,
		},
		{
			name:     "range with whitespace",
			pattern:  " 200 - 299 ",
			code:     250,
			expected: true,
		},

		// DNS response codes (0-based)
		{
			name:     "DNS success",
			pattern:  "0",
			code:     0,
			expected: true,
		},
		{
			name:     "DNS error range",
			pattern:  "1-5",
			code:     3,
			expected: true,
		},
		{
			name:     "DNS specific codes",
			pattern:  "0,2,3",
			code:     2,
			expected: true,
		},

		// Edge cases
		{
			name:     "invalid range format",
			pattern:  "200-",
			code:     200,
			expected: false,
		},
		{
			name:     "invalid number in range",
			pattern:  "abc-def",
			code:     200,
			expected: false,
		},
		{
			name:     "invalid single code",
			pattern:  "abc",
			code:     200,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := NewCodeMatcher(tt.pattern)
			result := matcher.Match(tt.code)
			
			if result != tt.expected {
				t.Errorf("Pattern %q with code %d: expected %v, got %v", 
					tt.pattern, tt.code, tt.expected, result)
			}
		})
	}
}

func TestCodeMatcherValidation(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		// Valid patterns
		{"single code", "200", true},
		{"range", "200-299", true},
		{"list", "200,201,202", true},
		{"mixed", "200,300-399", true},
		{"empty", "", true},
		{"whitespace", " 200 , 201 ", true},

		// Invalid patterns
		{"invalid range - no end", "200-", false},
		{"invalid range - no start", "-299", false},
		{"invalid range - too many parts", "200-250-299", false},
		{"invalid single code", "abc", false},
		{"mixed valid/invalid", "200,abc,300", false},
		{"invalid range numbers", "abc-def", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := NewCodeMatcher(tt.pattern)
			result := matcher.IsValid()
			
			if result != tt.expected {
				t.Errorf("Pattern %q validation: expected %v, got %v", 
					tt.pattern, tt.expected, result)
			}
		})
	}
}

func TestCodeMatcherExamples(t *testing.T) {
	matcher := NewCodeMatcher("")
	examples := matcher.Examples()
	
	// Verify we have some examples
	if len(examples) == 0 {
		t.Error("Expected some examples, got none")
	}
	
	// Verify each example is valid
	for _, example := range examples {
		testMatcher := NewCodeMatcher(example)
		if !testMatcher.IsValid() {
			t.Errorf("Example pattern %q should be valid", example)
		}
	}
}