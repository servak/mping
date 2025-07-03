package prober

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	ICMPV4 ProbeType = "icmpv4"
	ICMPV6 ProbeType = "icmpv6"
)

type (
	ICMPProber struct {
		version  ProbeType
		prefix   string // Custom prefix like "my-ping", "icmpv4", etc.
		c        *icmp.PacketConn
		body     []byte
		targets  map[*net.IPAddr]string // IPAddr -> original target string
		timeout  time.Duration
		runCnt   int
		runID    int
		tables   map[runTime]map[string]bool
		mu       sync.Mutex
		exitChan chan bool
		wg       sync.WaitGroup
	}

	ICMPConfig struct {
		Body            string `yaml:"body"`
		TOS             int    `yaml:"tos"`
		TTL             int    `yaml:"ttl"`
		SourceInterface string `yaml:"source_interface"`
	}

	runTime struct {
		runCnt   int
		sentTime time.Time
	}

	rcvdPkt struct {
		id, seq uint16
		target  string
	}
)

func NewICMPProber(t ProbeType, cfg *ICMPConfig, prefix string) (*ICMPProber, error) {
	var (
		c   *icmp.PacketConn
		err error
	)

	// Resolve source interface to IP address if specified
	sourceAddr, err := resolveSourceInterface(cfg.SourceInterface, t)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source interface: %v", err)
	}

	if t == ICMPV4 {
		c, err = icmp.ListenPacket("ip4:icmp", sourceAddr)
		if err != nil {
			return nil, err
		}
		p := c.IPv4PacketConn()
		if cfg.TOS != 0 {
			p.SetTOS(cfg.TOS)
		}
		if cfg.TTL != 0 {
			p.SetTTL(cfg.TTL)
		}
	} else {
		c, err = icmp.ListenPacket("ip6:ipv6-icmp", sourceAddr)
	}
	return &ICMPProber{
		version:  t,
		prefix:   prefix,
		c:        c,
		tables:   make(map[runTime]map[string]bool),
		targets:  make(map[*net.IPAddr]string),
		runID:    os.Getpid() & 0xffff,
		runCnt:   0,
		body:     []byte(cfg.Body),
		exitChan: make(chan bool),
	}, err
}

func (p *ICMPProber) Accept(target string) error {
	var hostname string

	// Check if it matches our prefix (e.g., "my-ping://host", "icmpv4://host")
	if strings.HasPrefix(target, p.prefix+"://") {
		hostname = strings.TrimPrefix(target, p.prefix+"://")
	} else if strings.HasPrefix(target, p.prefix+":") {
		// Legacy format (my-ping:host, icmpv4:host) - still supported
		hostname = strings.TrimPrefix(target, p.prefix+":")
	} else {
		return ErrNotAccepted
	}

	// Determine resolver type based on ICMP version
	resolvType := "ip4"
	if p.version == ICMPV6 {
		resolvType = "ip6"
	}

	// Resolve hostname to IP address
	ip, err := net.ResolveIPAddr(resolvType, hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve '%s': %w", hostname, err)
	}

	// Check for duplicate IP addresses
	ipStr := ip.String()
	for existingIP := range p.targets {
		if existingIP.String() == ipStr {
			return fmt.Errorf("duplicate target: %s resolves to already registered IP %s", hostname, ipStr)
		}
	}

	// Store target with original target string
	p.targets[ip] = target

	return nil
}

func (p *ICMPProber) addTable(runCnt int, sentTime time.Time) {
	rt := runTime{runCnt: runCnt, sentTime: sentTime}
	addrMap := make(map[string]bool, len(p.targets))
	for ip := range p.targets {
		addrMap[ip.String()] = false
	}
	p.mu.Lock()
	p.tables[rt] = addrMap
	p.mu.Unlock()
}

// getTargetInfo returns Key and DisplayName for the given IP address
func (p *ICMPProber) getTargetInfo(addr string) (string, string) {
	for ip, originalTarget := range p.targets {
		if ip.String() == addr {
			// Generate display name
			displayName := addr
			var hostname string

			// Extract hostname from various formats
			if strings.HasPrefix(originalTarget, p.prefix+"://") {
				hostname = strings.TrimPrefix(originalTarget, p.prefix+"://")
			} else if strings.HasPrefix(originalTarget, p.prefix+":") {
				hostname = strings.TrimPrefix(originalTarget, p.prefix+":")
			} else {
				hostname = originalTarget // plain hostname
			}

			if net.ParseIP(hostname) == nil {
				displayName = fmt.Sprintf("%s(%s)", hostname, addr)
			}
			return addr, displayName
		}
	}
	return addr, addr // fallback
}

func (p *ICMPProber) sent(r chan *Event, addr string) {
	key, displayName := p.getTargetInfo(addr)
	r <- &Event{
		Key:         key,
		DisplayName: displayName,
		Result:      SENT,
	}
}

func (p *ICMPProber) success(r chan *Event, runCnt int, addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; !ok {
			return
		}
		if table[addr] {
			continue
		}
		table[addr] = true
		elapse := time.Since(k.sentTime)
		key, displayName := p.getTargetInfo(addr)
		r <- &Event{
			Key:         key,
			DisplayName: displayName,
			Result:      SUCCESS,
			SentTime:    k.sentTime,
			Rtt:         elapse,
		}
		return
	}
}

func (p *ICMPProber) failed(r chan *Event, runCnt int, addr string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; ok {
			table[addr] = true
		}
		key, displayName := p.getTargetInfo(addr)
		r <- &Event{
			Key:         key,
			DisplayName: displayName,
			Result:      FAILED,
			SentTime:    k.sentTime,
			Rtt:         0,
			Message:     err.Error(),
		}
		return
	}
}

func (p *ICMPProber) checkTimeout(r chan *Event) {
	now := time.Now()
	var cleanTargets []runTime
	p.mu.Lock()
	defer p.mu.Unlock()
	for rt, table := range p.tables {
		if rt.sentTime.Add(p.timeout).After(now) {
			continue
		}
		for t, res := range table {
			if !res {
				key, displayName := p.getTargetInfo(t)
				r <- &Event{
					Key:         key,
					DisplayName: displayName,
					Result:      TIMEOUT,
					SentTime:    rt.sentTime,
					Rtt:         p.timeout,
					Message:     "timeout",
				}
			}
		}
		cleanTargets = append(cleanTargets, rt)
	}
	for _, rt := range cleanTargets {
		delete(p.tables, rt)
	}
}

func (p *ICMPProber) makeEchoMsg() icmp.Message {
	var t icmp.Type
	if p.version == ICMPV4 {
		t = ipv4.ICMPTypeEcho
	} else {
		t = ipv6.ICMPTypeEchoRequest
	}
	return icmp.Message{
		Type: t,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.runID,
			Seq:  p.runCnt,
			Data: p.body,
		},
	}
}

func (p *ICMPProber) probe(r chan *Event) {
	p.runCnt++
	if p.runCnt > 65535 {
		p.runCnt = 1
	}
	m := p.makeEchoMsg()

	b, err := m.Marshal(nil)
	if err != nil {
		fmt.Printf("Failed ICMP encode: %s\n", err)
		os.Exit(1)
	}

	n := time.Now()
	p.addTable(p.runCnt, n)
	for ip := range p.targets {
		_, err := p.c.WriteTo(b, ip)
		p.sent(r, ip.String()) // Use IP as event key
		if err != nil {
			p.failed(r, p.runCnt, ip.String(), err)
		}
	}
}

func (p *ICMPProber) recvPkts(r chan *Event) {
	pktbuf := make([]byte, maxPacketSize)
	for {
		n, ip, err := p.c.ReadFrom(pktbuf)
		if err != nil {
			fmt.Printf("Error reading ICMP packet: %s\n", err)
			os.Exit(1)
		}
		proto := ipv4.ICMPTypeEchoReply.Protocol()
		if p.version == ICMPV6 {
			proto = ipv6.ICMPTypeEchoReply.Protocol()
		}
		rm, err := icmp.ParseMessage(proto, pktbuf[:n])
		if err != nil {
			fmt.Printf("Error parsing ICMP message: %s\n", err)
			os.Exit(1)
		}
		offset := 0
		var pkt = rcvdPkt{
			target: ip.String(),
			id:     binary.BigEndian.Uint16(pktbuf[offset+4 : offset+6]),
			seq:    binary.BigEndian.Uint16(pktbuf[offset+6 : offset+8]),
		}
		if pkt.id != uint16(p.runID) {
			continue
		}
		if rm.Code == 0 {
			switch rm.Type {
			case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
				p.success(r, int(pkt.seq), pkt.target)
			}
		}
	}
}

func (p *ICMPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	p.timeout = timeout
	ticker := time.NewTicker(interval)
	go p.recvPkts(r)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.probe(r)
		for {
			select {
			case <-p.exitChan:
				return
			case <-ticker.C:
				p.probe(r)
				go p.checkTimeout(r)
			}
		}
	}()
	p.wg.Wait()
	for {
		p.checkTimeout(r)
		if len(p.tables) == 0 {
			break
		}
		time.Sleep(interval)
	}
	return nil
}

func (p *ICMPProber) Stop() {
	p.exitChan <- true
}

// resolveSourceInterface resolves interface name or IP address to a bind address
func resolveSourceInterface(sourceInterface string, probeType ProbeType) (string, error) {
	if sourceInterface == "" {
		// Return default bind address for the protocol
		if probeType == ICMPV4 {
			return "0.0.0.0", nil
		}
		return "::", nil
	}

	// Try to parse as IP address first
	ip := net.ParseIP(sourceInterface)
	if ip != nil {
		// Validate IP version matches probe type
		if probeType == ICMPV4 && ip.To4() == nil {
			return "", fmt.Errorf("IPv4 address required for ICMPv4, got: %s", sourceInterface)
		}
		if probeType == ICMPV6 && ip.To4() != nil {
			return "", fmt.Errorf("IPv6 address required for ICMPv6, got: %s", sourceInterface)
		}
		return sourceInterface, nil
	}

	// Try to resolve as interface name
	iface, err := net.InterfaceByName(sourceInterface)
	if err != nil {
		return "", fmt.Errorf("interface not found: %s", sourceInterface)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("failed to get addresses for interface %s: %v", sourceInterface, err)
	}

	// Find appropriate IP address for the probe type
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		if probeType == ICMPV4 && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
		if probeType == ICMPV6 && ipNet.IP.To4() == nil && !ipNet.IP.IsLoopback() {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no suitable %s address found on interface %s",
		map[ProbeType]string{ICMPV4: "IPv4", ICMPV6: "IPv6"}[probeType], sourceInterface)
}
