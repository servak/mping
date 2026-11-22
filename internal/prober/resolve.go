package prober

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Name resolution of the targets: like the functions of package net that take
// no context (ResolveIPAddr, ResolveUDPAddr, LookupIP), with the same choice
// among the addresses of a name, but giving up when a context is done.

// resolver is the part of *net.Resolver that targets need. It is called for
// names only, never for IP addresses. Tests replace it.
type resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// isName reports whether host has to be looked up, as opposed to an IP address
// (with or without a zone) and the empty host, which package net treats
// specially.
func isName(host string) bool {
	if host == "" {
		return false
	}
	_, err := netip.ParseAddr(host)
	return err != nil
}

// resolveIPAddr is net.ResolveIPAddr for network "ip" (IPv4 preferred), "ip4"
// or "ip6". That the empty host is the unspecified address is part of that.
func resolveIPAddr(ctx context.Context, r resolver, network, host string) (*net.IPAddr, error) {
	if !isName(host) {
		return net.ResolveIPAddr(network, host) // looks nothing up
	}
	ips, err := r.LookupIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errNoAddress(host)
	}
	// The choice of net.addrList.forResolve: for "ip", IPv4 unless the host
	// looks like an IPv6 address
	ip, want4 := ips[0], network == "ip" && !strings.Contains(host, ":")
	for _, a := range ips {
		if want4 == (a.To4() != nil) {
			ip = a
			break
		}
	}
	addr := &net.IPAddr{IP: ip}
	if ip.To4() == nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast()) {
		// LookupIP, which unlike LookupIPAddr can ask for one family only,
		// drops the zones. /etc/hosts and mDNS yield link-local addresses.
		addrs, err := r.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			if a.IP.Equal(ip) {
				addr.Zone = a.Zone
				break
			}
		}
	}
	return addr, nil
}

// resolveUDPAddr is net.ResolveUDPAddr("udp", address) for an address with a
// numeric port.
func resolveUDPAddr(ctx context.Context, r resolver, address string) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if !isName(host) {
		return net.ResolveUDPAddr("udp", address) // looks nothing up
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, &net.AddrError{Err: "invalid port", Addr: port}
	}
	ip, err := resolveIPAddr(ctx, r, "ip", host)
	if err != nil {
		return nil, err
	}
	return &net.UDPAddr{IP: ip.IP, Port: int(n), Zone: ip.Zone}, nil
}

// lookupFirstIP returns the first address of net.LookupIP, which has no zone
// and is of either family.
func lookupFirstIP(ctx context.Context, r resolver, host string) (net.IP, error) {
	if !isName(host) {
		ips, err := net.LookupIP(host) // looks nothing up
		if err != nil {
			return nil, err
		}
		return ips[0], nil
	}
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errNoAddress(host)
	}
	return addrs[0].IP, nil
}

// errNoAddress is for a resolver that returns neither addresses nor an error.
func errNoAddress(host string) error {
	return fmt.Errorf("lookup %s: no address", host)
}
