package prober

import (
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"time"
)

const (
	TCP ProbeType = "tcp"
)

type (
	TCPProber struct {
		targets map[string]tcpTarget // key (target as given, also its display name) -> address and source
		config  *TCPConfig
		// sources caches the source address per family (index 1 for IPv6)
		sources [2]*net.TCPAddr
		prefix  string
		loop    *runLoop
	}

	tcpTarget struct {
		addr  string       // resolved ip:port to dial
		local *net.TCPAddr // source address; nil without source_interface
	}

	TCPConfig struct {
		SourceInterface string `yaml:"source_interface,omitempty"`
	}
)

// Validate validates the TCP configuration
func (cfg *TCPConfig) Validate() error {
	// Basic validation - TCP config is simple, no specific validation needed for now
	return nil
}

func NewTCPProber(cfg *TCPConfig, prefix string) *TCPProber {
	return &TCPProber{
		targets: make(map[string]tcpTarget),
		config:  cfg,
		prefix:  prefix,
		loop:    newRunLoop(),
	}
}

func (p *TCPProber) Accept(target string) error {
	if _, ok := cutPrefix(target, p.prefix); !ok {
		return ErrNotAccepted
	}

	host, port, err := p.parseTarget(target)
	if err != nil {
		return fmt.Errorf("invalid TCP target: %w", err)
	}

	// DNS解決を事前に実行
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve '%s': %w", host, err)
	}

	// 最初のIPを使用
	ip := ips[0]
	ipPort := net.JoinHostPort(ip.String(), port)

	// Resolve the source address now, so that an unusable source_interface
	// fails at startup rather than every probe
	var local *net.TCPAddr
	if p.config != nil && p.config.SourceInterface != "" {
		if local, err = p.localAddr(ipPort); err != nil {
			return fmt.Errorf("source_interface %q for %s: %w", p.config.SourceInterface, target, err)
		}
	}
	p.targets[target] = tcpTarget{addr: ipPort, local: local}

	return nil
}

func (p *TCPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	return runPeriodic(p.loop, result, TCP, interval, slices.Sorted(maps.Keys(p.targets)),
		func(target string) (string, string) { return target, target },
		func(target string) { p.sendProbe(result, target, timeout) })
}

// Stop stops probing and waits for Start to return. It is safe to call more than once.
func (p *TCPProber) Stop() {
	p.loop.stop()
}

func (p *TCPProber) sendProbe(result chan *Event, target string, timeout time.Duration) {
	// target is the key given to Accept; the address was resolved there
	now := time.Now()
	t := p.targets[target]
	name := target
	result <- sentEvent(target, name, now)

	// Create dialer with timeout, bound to the source address resolved by Accept
	dialer := &net.Dialer{
		Timeout: timeout,
	}
	if t.local != nil {
		dialer.LocalAddr = t.local
	}

	// Attempt TCP connection using pre-resolved IP:port
	conn, err := dialer.Dial("tcp", t.addr)
	rtt := time.Since(now)

	if err != nil {
		result <- errorEvent(target, name, now, err)
		return
	}

	// Close connection immediately after successful establishment
	conn.Close()
	result <- successEvent(target, name, now, rtt, nil) // TCP has no details
}

// lookupInterfaceIP is interfaceIP, replaceable in tests.
var lookupInterfaceIP = interfaceIP

// localAddr returns the source address for target (an "ip:port" from
// Accept) in its address family, resolved once per family. The configured
// source_interface is an IP address, which must be of the target's family,
// or an interface name, whose address of that family is used (a global
// unicast one preferred).
func (p *TCPProber) localAddr(target string) (*net.TCPAddr, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(host) // may carry a zone
	if err != nil {
		return nil, err
	}
	v6 := ip.Is6() && !ip.Is4In6()
	fam := 0
	if v6 {
		fam = 1
	}
	if a := p.sources[fam]; a != nil {
		return a, nil
	}
	a, err := resolveTCPSource(p.config.SourceInterface, v6)
	if err != nil {
		return nil, err
	}
	p.sources[fam] = a
	return a, nil
}

// resolveTCPSource resolves a source_interface (an IP address or an
// interface name) to a TCP source address of the given family.
func resolveTCPSource(src string, v6 bool) (*net.TCPAddr, error) {
	if ip, err := netip.ParseAddr(src); err == nil {
		if (ip.Is6() && !ip.Is4In6()) != v6 {
			fam := "IPv4"
			if v6 {
				fam = "IPv6"
			}
			return nil, fmt.Errorf("source address %s cannot reach an %s target", src, fam)
		}
		return &net.TCPAddr{IP: net.IP(ip.Unmap().AsSlice()), Zone: ip.Zone()}, nil
	}
	a, err := lookupInterfaceIP(src, v6, true)
	if err != nil {
		return nil, err
	}
	return &net.TCPAddr{IP: a.IP, Zone: a.Zone}, nil
}

func (p *TCPProber) parseTarget(target string) (host, port string, err error) {
	target, _ = cutPrefix(target, p.prefix)

	// Parse host:port
	host, port, err = net.SplitHostPort(target)
	if err != nil {
		return "", "", fmt.Errorf("invalid target format: %s", target)
	}

	return host, port, nil
}
