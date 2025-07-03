package prober

import (
	"strconv"
	"strings"
)

// CodeMatcher provides flexible code matching capabilities
// Supports single codes, comma-separated lists, and ranges
type CodeMatcher struct {
	pattern string
}

// NewCodeMatcher creates a new code matcher with the given pattern
func NewCodeMatcher(pattern string) *CodeMatcher {
	return &CodeMatcher{pattern: strings.TrimSpace(pattern)}
}

// Match checks if the given code matches the configured pattern
// Supports:
// - Single code: "200"
// - Comma-separated list: "200,201,202"
// - Range: "200-299", "400-499"
// - Mixed: "200,300-399,404"
func (m *CodeMatcher) Match(code int) bool {
	if m.pattern == "" {
		return true // Empty pattern matches any code
	}

	// Split by commas to handle multiple patterns
	patterns := strings.Split(m.pattern, ",")
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if m.matchSinglePattern(code, pattern) {
			return true
		}
	}
	return false
}

// matchSinglePattern matches a code against a single pattern (no commas)
func (m *CodeMatcher) matchSinglePattern(code int, pattern string) bool {
	// Handle range pattern: "200-299"
	if strings.Contains(pattern, "-") {
		return m.matchRange(code, pattern)
	}

	// Handle single code: "200"
	if expectedCode, err := strconv.Atoi(pattern); err == nil {
		return code == expectedCode
	}

	return false
}

// matchRange matches a code against a range pattern like "200-299"
func (m *CodeMatcher) matchRange(code int, pattern string) bool {
	parts := strings.Split(pattern, "-")
	if len(parts) != 2 {
		return false
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	start, err1 := strconv.Atoi(startStr)
	end, err2 := strconv.Atoi(endStr)

	if err1 != nil || err2 != nil {
		return false
	}

	return code >= start && code <= end
}

// IsValid checks if the pattern is valid
func (m *CodeMatcher) IsValid() bool {
	if m.pattern == "" {
		return true
	}

	patterns := strings.Split(m.pattern, ",")
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if !m.isValidSinglePattern(pattern) {
			return false
		}
	}
	return true
}

// isValidSinglePattern validates a single pattern
func (m *CodeMatcher) isValidSinglePattern(pattern string) bool {
	if pattern == "" {
		return false
	}

	// Range pattern
	if strings.Contains(pattern, "-") {
		parts := strings.Split(pattern, "-")
		if len(parts) != 2 {
			return false
		}
		_, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		_, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		return err1 == nil && err2 == nil
	}

	// Single code pattern
	_, err := strconv.Atoi(pattern)
	return err == nil
}

// Examples returns example patterns for documentation
func (m *CodeMatcher) Examples() []string {
	return []string{
		"200",           // Single code
		"200,201,202",   // Multiple codes
		"200-299",       // Range
		"200,300-399",   // Mixed
		"0-99,200-299",  // Multiple ranges
	}
}