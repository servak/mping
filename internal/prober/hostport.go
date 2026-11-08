package prober

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// splitServerPort splits a "server[:port]" string. Unlike net.SplitHostPort it
// also accepts a missing port and IPv6 literals in every common spelling:
// "[::1]:123", "[::1]" and the bare "::1". port is empty when none was given.
//
// A bare IPv6 literal is always an address with no port: "2001:db8::1:123" is
// the address 2001:db8::1:123, not 2001:db8::1 on port 123. Following the
// usual convention, a port needs brackets: "[2001:db8::1]:123".
func splitServerPort(s string) (host, port string, err error) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", fmt.Errorf("invalid server:port format: missing ']' in %q", s)
		}
		host = s[1:end]
		rest := s[end+1:]
		switch {
		case rest == "":
			return host, "", nil
		case strings.HasPrefix(rest, ":") && len(rest) > 1:
			return host, rest[1:], nil
		default:
			return "", "", fmt.Errorf("invalid server:port format: unexpected %q after ']'", rest)
		}
	}
	switch strings.Count(s, ":") {
	case 0:
		return s, "", nil
	case 1:
		h, p, err := net.SplitHostPort(s)
		if err != nil {
			return "", "", fmt.Errorf("invalid server:port format: %w", err)
		}
		return h, p, nil
	default:
		// More than one colon can only be a bare IPv6 literal.
		if _, err := netip.ParseAddr(s); err != nil {
			return "", "", fmt.Errorf("invalid server:port format: %q is not an IPv6 address (wrap it in [] to add a port)", s)
		}
		return s, "", nil
	}
}
