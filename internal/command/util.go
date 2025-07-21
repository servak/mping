package command

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func parseBrackets(_hosts []string) []string {
	hosts := []string{}
	bracketRegex := regexp.MustCompile(`\[([^\]]+)\]`)

	for _, h := range _hosts {
		matches := bracketRegex.FindAllStringSubmatch(h, -1)
		if len(matches) == 0 {
			hosts = append(hosts, h)
			continue
		}

		// Generate all combinations for this host
		expanded := expandHost(h, matches)
		hosts = append(hosts, expanded...)
	}

	return hosts
}

func expandHost(host string, matches [][]string) []string {
	results := []string{host}

	for _, match := range matches {
		fullMatch := match[0] // [1-10] or [web,db,cache]
		content := match[1]   // 1-10 or web,db,cache

		var expansions []string
		var isValidExpansion bool

		if strings.Contains(content, "-") && !strings.Contains(content, ",") {
			// Range expansion: [1-10], [01-05]
			expansions = expandRange(content)
			// Check if expansion was successful (not just returning original content)
			isValidExpansion = !(len(expansions) == 1 && expansions[0] == content)
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
		var newResults []string
		for _, result := range results {
			for _, expansion := range expansions {
				newResults = append(newResults, strings.Replace(result, fullMatch, expansion, 1))
			}
		}
		results = newResults
	}

	return results
}

func expandRange(content string) []string {
	parts := strings.Split(content, "-")
	if len(parts) != 2 {
		return []string{content} // Invalid range format
	}

	startStr, endStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

	// Try to parse as integers
	start, startErr := strconv.Atoi(startStr)
	end, endErr := strconv.Atoi(endStr)

	if startErr != nil || endErr != nil {
		// Try character range (a-z, A-Z)
		if len(startStr) == 1 && len(endStr) == 1 {
			return expandCharRange(startStr[0], endStr[0])
		}
		return []string{content} // Invalid range
	}

	if start > end {
		return []string{content} // Invalid range
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

	return result
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

func parseCidr(_hosts []string) []string {
	hosts := []string{}
	for _, h := range _hosts {
		ip, ipnet, err := net.ParseCIDR(h)
		if err != nil {
			hosts = append(hosts, h)
			continue
		}

		for i := ip.Mask(ipnet.Mask); ipnet.Contains(i); ipInc(i) {
			hosts = append(hosts, i.String())
		}
	}

	return hosts
}

func ipInc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func file2hostnames(fp *os.File) []string {
	hosts := []string{}
	reader := bufio.NewReaderSize(fp, 4096)
	// Fixed regex: only match # or ; at start of line or after whitespace
	// This prevents breaking URLs like http://example.com
	r := regexp.MustCompile(`(?:^|\s)[#;].*`)

	for {
		lb, _, err := reader.ReadLine()
		if errors.Is(err, io.EOF) {
			break
		}

		line := r.ReplaceAllString(string(lb), "")
		line = strings.Trim(line, "\t \n")
		if line == "" {
			continue
		}
		hosts = append(hosts, line)
	}

	return hosts
}

func parseHostnames(args []string, fpath string) []string {
	hosts := []string{}

	// Only attempt to open file if path is not empty
	if fpath != "" {
		fp, err := os.Open(fpath)
		if err == nil {
			hosts = file2hostnames(fp)
			fp.Close() // Critical fix: close file to prevent resource leak
		}
	}

	hosts = append(hosts, args...)
	hosts = parseBrackets(hosts)
	return parseCidr(hosts)
}
