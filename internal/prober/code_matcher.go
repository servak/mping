package prober

import (
	"strconv"
	"strings"
)

// MatchCode checks if the given code matches the pattern
// Supports:
// - Single code: "200"
// - Comma-separated list: "200,201,202"
// - Range: "200-299", "400-499"
// - Mixed: "200,300-399,404"
func MatchCode(code int, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true // Empty pattern matches any code
	}

	// Split by commas to handle multiple patterns
	patterns := strings.Split(pattern, ",")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if matchSinglePattern(code, p) {
			return true
		}
	}
	return false
}

// matchSinglePattern matches a code against a single pattern (no commas)
func matchSinglePattern(code int, pattern string) bool {
	// Handle range pattern: "200-299"
	if strings.Contains(pattern, "-") {
		return matchRange(code, pattern)
	}

	// Handle single code: "200"
	if expectedCode, err := strconv.Atoi(pattern); err == nil {
		return code == expectedCode
	}

	return false
}

// matchRange matches a code against a range pattern like "200-299"
func matchRange(code int, pattern string) bool {
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

// IsValidCodePattern checks if the pattern is valid
func IsValidCodePattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}

	patterns := strings.Split(pattern, ",")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if !isValidSinglePattern(p) {
			return false
		}
	}
	return true
}

// isValidSinglePattern validates a single pattern
func isValidSinglePattern(pattern string) bool {
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