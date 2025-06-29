package prober

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ProbeTarget represents a unified probe target structure
type ProbeTarget struct {
	// Basic URL components
	Scheme   string            // Protocol: icmpv4, icmpv6, http, https, tcp, dns, ntp
	Host     string            // Hostname or IP address
	Port     string            // Port number (empty for default)
	Path     string            // Path component (for DNS queries, HTTP paths, etc.)
	Query    map[string]string // Query parameters
	
	// Original input for debugging
	Original string
}

// ParseTarget parses various target formats into a unified ProbeTarget structure
func ParseTarget(input string) (*ProbeTarget, error) {
	// Handle legacy colon-separated format (icmpv4:host, icmpv6:host)
	if isLegacyFormat(input) {
		return parseLegacyFormat(input)
	}

	// Handle URL format (http://, https://, tcp://, dns://, ntp://, etc.)
	if isURLFormat(input) {
		return parseURLFormat(input)
	}

	// Default to ICMP probe for bare hostnames/IPs
	return parseDefaultICMP(input)
}

// isLegacyFormat checks if the input uses legacy colon-separated format
func isLegacyFormat(input string) bool {
	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 {
		return false
	}
	
	scheme := parts[0]
	return scheme == string(ICMPV4) || scheme == string(ICMPV6)
}

// isURLFormat checks if the input uses URL format (scheme://)
func isURLFormat(input string) bool {
	return strings.Contains(input, "://")
}

// parseLegacyFormat parses legacy colon-separated format
func parseLegacyFormat(input string) (*ProbeTarget, error) {
	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid legacy format: %s", input)
	}

	return &ProbeTarget{
		Scheme:   parts[0],
		Host:     parts[1],
		Original: input,
		Query:    make(map[string]string),
	}, nil
}

// parseURLFormat parses URL format using net/url
func parseURLFormat(input string) (*ProbeTarget, error) {
	u, err := url.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %s (%w)", input, err)
	}

	target := &ProbeTarget{
		Scheme:   u.Scheme,
		Host:     u.Hostname(),
		Port:     u.Port(),
		Path:     u.Path,
		Original: input,
		Query:    make(map[string]string),
	}

	// Parse query parameters
	for key, values := range u.Query() {
		if len(values) > 0 {
			target.Query[key] = values[0]
		}
	}

	return target, nil
}

// parseDefaultICMP creates ICMP target for bare hostname/IP
func parseDefaultICMP(input string) (*ProbeTarget, error) {
	// Determine IP version
	scheme := string(ICMPV4)
	if addr := net.ParseIP(input); addr != nil && addr.To4() == nil {
		scheme = string(ICMPV6)
	}

	return &ProbeTarget{
		Scheme:   scheme,
		Host:     input,
		Original: input,
		Query:    make(map[string]string),
	}, nil
}

// GetPort returns the port with default fallback
func (t *ProbeTarget) GetPort() string {
	if t.Port != "" {
		return t.Port
	}
	
	// Return default ports for known schemes
	switch t.Scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	case "dns":
		return "53"
	case "ntp":
		return "123"
	default:
		return ""
	}
}

// GetPortInt returns the port as integer with default fallback
func (t *ProbeTarget) GetPortInt() int {
	portStr := t.GetPort()
	if portStr == "" {
		return 0
	}
	
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// HostPort returns host:port format for network operations
func (t *ProbeTarget) HostPort() string {
	port := t.GetPort()
	if port == "" {
		return t.Host
	}
	return net.JoinHostPort(t.Host, port)
}

// String returns a string representation of the target
func (t *ProbeTarget) String() string {
	if t.Port != "" {
		return fmt.Sprintf("%s://%s:%s%s", t.Scheme, t.Host, t.Port, t.Path)
	}
	return fmt.Sprintf("%s://%s%s", t.Scheme, t.Host, t.Path)
}

// DisplayName returns a user-friendly display name
func (t *ProbeTarget) DisplayName() string {
	switch t.Scheme {
	case string(ICMPV4), string(ICMPV6):
		return t.Host
	case "tcp":
		return t.HostPort()
	case "http", "https":
		if t.Path != "" && t.Path != "/" {
			return fmt.Sprintf("%s%s", t.Host, t.Path)
		}
		return t.Host
	case "dns":
		if domain := t.Query["domain"]; domain != "" {
			recordType := t.Query["type"]
			if recordType == "" {
				recordType = "A"
			}
			return fmt.Sprintf("%s(%s/%s)", t.Host, domain, recordType)
		}
		// Fallback to path-based DNS format
		if t.Path != "" {
			return fmt.Sprintf("%s%s", t.Host, t.Path)
		}
		return t.Host
	case "ntp":
		return t.Host
	default:
		return t.HostPort()
	}
}

// IsCompatibleWith checks if the target is compatible with a given probe type
func (t *ProbeTarget) IsCompatibleWith(probeType ProbeType) bool {
	return t.Scheme == string(probeType)
}

// ParseTargets parses multiple target strings into ProbeTarget structs
func ParseTargets(inputs []string) ([]*ProbeTarget, error) {
	var targets []*ProbeTarget
	var errors []string

	for _, input := range inputs {
		target, err := ParseTarget(input)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to parse '%s': %v", input, err))
			continue
		}
		targets = append(targets, target)
	}

	if len(errors) > 0 {
		return targets, fmt.Errorf("parsing errors: %s", strings.Join(errors, "; "))
	}

	return targets, nil
}

// GroupTargetsByScheme groups targets by their scheme (protocol)
func GroupTargetsByScheme(targets []*ProbeTarget) map[string][]*ProbeTarget {
	groups := make(map[string][]*ProbeTarget)

	for _, target := range targets {
		groups[target.Scheme] = append(groups[target.Scheme], target)
	}

	return groups
}

// ExtractDisplayTargets extracts display names from targets for the given scheme
func ExtractDisplayTargets(targets []*ProbeTarget, scheme string) []string {
	var displays []string

	for _, target := range targets {
		if target.Scheme == scheme {
			displays = append(displays, target.DisplayName())
		}
	}

	return displays
}

// ExtractOriginalTargets extracts original target strings for the given scheme
func ExtractOriginalTargets(targets []*ProbeTarget, scheme string) []string {
	var originals []string

	for _, target := range targets {
		if target.Scheme == scheme {
			originals = append(originals, target.Original)
		}
	}

	return originals
}

// ConvertToLegacyFormat converts ProbeTarget back to legacy string format
// This is for backward compatibility with existing Prober implementations
func (t *ProbeTarget) ConvertToLegacyFormat() string {
	switch t.Scheme {
	case string(ICMPV4), string(ICMPV6):
		return fmt.Sprintf("%s:%s", t.Scheme, t.Host)
	case "http", "https":
		return t.String() // Return full URL format: http://host:port/path
	case "tcp":
		return t.String() // tcp://host:port
	default:
		return t.String()
	}
}