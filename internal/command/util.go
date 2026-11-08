package command

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// maxTargets caps how many targets a single expansion (bracket range or CIDR)
// and the overall target list may produce, so that typos such as 0.0.0.0/0 or
// [1-100000000] fail fast instead of exhausting memory.
const maxTargets = 65536

// ExpandTargets processes target specifications through a pipeline of expansions
func ExpandTargets(args []string, filePath string) ([]string, error) {
	targets, err := collectTargets(args, filePath)
	if err != nil {
		return nil, err
	}
	if targets, err = parseBrackets(targets); err != nil {
		return nil, err
	}
	if targets, err = parseCIDR(targets); err != nil {
		return nil, err
	}
	if len(targets) > maxTargets {
		return nil, errTooManyTargets(len(targets))
	}
	return targets, nil
}

func parseBrackets(_hosts []string) ([]string, error) {
	hosts := []string{}
	bracketRegex := regexp.MustCompile(`\[([^\]]+)\]`)

	for _, h := range _hosts {
		if len(hosts) > maxTargets {
			return nil, errTooManyTargets(len(hosts))
		}
		matches := bracketRegex.FindAllStringSubmatch(h, -1)
		if len(matches) == 0 {
			hosts = append(hosts, h)
			continue
		}

		// Generate all combinations for this host
		expanded, err := expandHost(h, matches)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, expanded...)
		if len(hosts) > maxTargets {
			return nil, errTooManyTargets(len(hosts))
		}
	}

	return hosts, nil
}

func errTooManyTargets(n int) error {
	return fmt.Errorf("too many targets (%d): expansion is limited to %d", n, maxTargets)
}

func expandHost(host string, matches [][]string) ([]string, error) {
	results := []string{host}

	for _, match := range matches {
		fullMatch := match[0] // [1-10] or [web,db,cache]
		content := match[1]   // 1-10 or web,db,cache

		// Keep bracketed IPv6 literals such as tcp://[::1]:80 or
		// http://[2001:db8::1]/ intact; they are address syntax, not expansions.
		if isIPv6Literal(content) {
			continue
		}

		var expansions []string
		var isValidExpansion bool

		if strings.Contains(content, "-") && !strings.Contains(content, ",") {
			// Range expansion: [1-10], [01-05]
			var err error
			expansions, err = expandRange(content)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", host, err)
			}
			// Check if expansion was successful (not just returning original content)
			isValidExpansion = len(expansions) != 1 || expansions[0] != content
		} else if strings.Contains(content, ",") {
			// List expansion: [web,db,cache]
			expansions = expandList(content)
			isValidExpansion = true
		} else {
			// Single value: [5]
			expansions = []string{content}
			isValidExpansion = true
		}

		// If expansion failed, skip this match and keep original brackets
		if !isValidExpansion {
			continue
		}

		// Apply expansions to all current results
		if len(results)*len(expansions) > maxTargets {
			return nil, fmt.Errorf("%s: %w", host, errTooManyTargets(len(results)*len(expansions)))
		}
		var newResults []string
		for _, result := range results {
			for _, expansion := range expansions {
				newResults = append(newResults, strings.Replace(result, fullMatch, expansion, 1))
			}
		}
		results = newResults
	}

	return results, nil
}

func expandRange(content string) ([]string, error) {
	parts := strings.Split(content, "-")
	if len(parts) != 2 {
		return []string{content}, nil // Invalid range format
	}

	startStr, endStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

	// Try to parse as integers
	start, startErr := strconv.Atoi(startStr)
	end, endErr := strconv.Atoi(endStr)

	if startErr != nil || endErr != nil {
		// Try character range (a-z, A-Z)
		if len(startStr) == 1 && len(endStr) == 1 {
			return expandCharRange(startStr[0], endStr[0]), nil
		}
		return []string{content}, nil // Invalid range
	}

	if start > end {
		return []string{content}, nil // Invalid range
	}
	if end-start >= maxTargets {
		return nil, errTooManyTargets(end - start + 1)
	}

	// Determine zero-padding from the start string
	padding := len(startStr)
	if startStr[0] != '0' && start != 0 {
		padding = 0 // No padding unless starts with 0
	}

	var result []string
	for i := start; i <= end; i++ {
		if padding > 0 {
			result = append(result, fmt.Sprintf("%0*d", padding, i))
		} else {
			result = append(result, strconv.Itoa(i))
		}
	}

	return result, nil
}

func expandCharRange(start, end byte) []string {
	if start > end {
		return []string{string(start) + "-" + string(end)} // Invalid range
	}

	var result []string
	for i := start; i <= end; i++ {
		result = append(result, string(i))
	}

	return result
}

func expandList(content string) []string {
	parts := strings.Split(content, ",")
	var result []string
	for _, part := range parts {
		result = append(result, strings.TrimSpace(part))
	}
	return result
}

func parseCIDR(_hosts []string) ([]string, error) {
	hosts := []string{}
	for _, h := range _hosts {
		ip, ipnet, err := net.ParseCIDR(h)
		if err != nil {
			hosts = append(hosts, h)
			continue
		}

		ones, bits := ipnet.Mask.Size()
		hostBits := bits - ones
		if hostBits > 16 { // 2^16 == maxTargets
			return nil, fmt.Errorf("%s: too many targets (2^%d addresses): expansion is limited to %d", h, hostBits, maxTargets)
		}
		count := 1 << hostBits
		if len(hosts)+count > maxTargets {
			return nil, fmt.Errorf("%s: %w", h, errTooManyTargets(len(hosts)+count))
		}

		// Iterate by count rather than ipnet.Contains so the last address of
		// the address space cannot wrap around and loop forever.
		cur := ip.Mask(ipnet.Mask)
		for i := 0; i < count; i++ {
			hosts = append(hosts, cur.String())
			ipInc(cur)
		}
	}

	return hosts, nil
}

func ipInc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func file2hostnames(fp *os.File) ([]string, error) {
	hosts := []string{}
	scanner := bufio.NewScanner(fp)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// Fixed regex: only match # or ; at start of line or after whitespace
	// This prevents breaking URLs like http://example.com
	r := regexp.MustCompile(`(?:^|\s)[#;].*`)

	for scanner.Scan() {
		line := r.ReplaceAllString(scanner.Text(), "")
		line = strings.Trim(line, "\t \n\r")
		if line == "" {
			continue
		}
		hosts = append(hosts, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return hosts, nil
}

// collectTargets gathers targets from command-line arguments and optional file
func collectTargets(args []string, filePath string) ([]string, error) {
	targets := []string{}

	// Load targets from file if specified
	if filePath != "" {
		fp, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open target file: %w", err)
		}
		targets, err = file2hostnames(fp)
		fp.Close() // Critical fix: close file to prevent resource leak
		if err != nil {
			return nil, fmt.Errorf("failed to read target file %q: %w", filePath, err)
		}
	}

	// Append command-line arguments
	targets = append(targets, args...)
	return targets, nil
}

// isIPv6Literal reports whether s is an IPv6 address, optionally with a zone
// (e.g. "::1", "2001:db8::1", "fe80::1%eth0").
func isIPv6Literal(s string) bool {
	if !strings.Contains(s, ":") {
		return false
	}
	addr, _, _ := strings.Cut(s, "%")
	return net.ParseIP(addr) != nil
}
