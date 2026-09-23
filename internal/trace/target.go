package trace

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// HostFromTarget extracts the host to trace from an mping target or display
// name. It understands:
//
//	8.8.8.8, example.com             plain hosts
//	example.com(93.184.216.34)       ICMP display names (the IP is used)
//	icmpv6://example.com             forces IPv6
//	tcp://host:443, https://host/p   URL-style targets of any prober
//	dns://8.8.8.8/example.com/A      the DNS server is traced
//
// preferV6 reports whether the target explicitly asks for IPv6.
func HostFromTarget(target string) (host string, preferV6 bool) {
	target = strings.TrimSpace(target)

	// ICMP display names: "name(ip)"
	if open := strings.LastIndex(target, "("); open > 0 && strings.HasSuffix(target, ")") {
		if ip := net.ParseIP(target[open+1 : len(target)-1]); ip != nil {
			return ip.String(), ip.To4() == nil
		}
	}

	if scheme, rest, ok := strings.Cut(target, "://"); ok {
		preferV6 = scheme == "icmpv6"
		if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
			return u.Hostname(), preferV6
		}
		// dns:///example.com has an empty host; fall back to the raw rest
		rest = strings.TrimPrefix(rest, "/")
		host, _, _ = strings.Cut(rest, "/")
		return stripPort(host), preferV6
	}

	// Legacy "icmpv4:host" / "icmpv6:host" (but not bare IPv6 addresses)
	if net.ParseIP(target) == nil {
		if scheme, rest, ok := strings.Cut(target, ":"); ok && strings.HasPrefix(scheme, "icmp") {
			return rest, scheme == "icmpv6"
		}
	}
	return stripPort(target), false
}

func stripPort(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// Resolve resolves host to an IP address, preferring IPv4 unless preferV6.
func Resolve(host string, preferV6 bool) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	order := []string{"ip4", "ip6"}
	if preferV6 {
		order = []string{"ip6", "ip4"}
	}
	var lastErr error
	for _, network := range order {
		addr, err := net.ResolveIPAddr(network, host)
		if err == nil {
			return addr.IP, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("failed to resolve %q: %w", host, lastErr)
}
