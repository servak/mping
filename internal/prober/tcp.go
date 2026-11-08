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
		targets map[string]string // key (ip:port) -> displayName (host:port)
		config  *TCPConfig
		prefix  string
		loop    *runLoop
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
		targets: make(map[string]string),
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

	p.targets[ipPort] = target // key -> displayName mapping

	return nil
}

func (p *TCPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	return runPeriodic(p.loop, result, TCP, interval, slices.Sorted(maps.Keys(p.targets)),
		func(target string) (string, string) { return target, p.targets[target] },
		func(target string) { p.sendProbe(result, target, timeout) })
}

// Stop stops probing and waits for Start to return. It is safe to call more than once.
func (p *TCPProber) Stop() {
	p.loop.stop()
}

func (p *TCPProber) sendProbe(result chan *Event, target string, timeout time.Duration) {
	// target is already in "ip:port" format from Accept method
	now := time.Now()
	name := p.targets[target]
	result <- sentEvent(target, name, now)

	// Create dialer with timeout
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	// Set source interface if specified
	if p.config.SourceInterface != "" {
		local, err := p.localAddr(target)
		if err != nil {
			result <- failureEvent(target, name, FAILED, now, 0, err.Error())
			return
		}
		dialer.LocalAddr = local
	}

	// Attempt TCP connection using pre-resolved IP:port
	conn, err := dialer.Dial("tcp", target)
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

// localAddr returns the source address on the configured interface in the
// address family of target (an "ip:port" from Accept).
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
	a, err := lookupInterfaceIP(p.config.SourceInterface, v6, true)
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
