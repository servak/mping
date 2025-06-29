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
		c        *icmp.PacketConn
		body     []byte
		targets  []*net.IPAddr
		targetMap map[string]string  // IP -> displayName mapping
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

func NewICMPProber(t ProbeType, cfg *ICMPConfig) (*ICMPProber, error) {
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
		version:   t,
		c:         c,
		tables:    make(map[runTime]map[string]bool),
		targets:   make([]*net.IPAddr, 0),
		targetMap: make(map[string]string),
		runID:     os.Getpid() & 0xffff,
		runCnt:    0,
		body:      []byte(cfg.Body),
		exitChan:  make(chan bool),
	}, err
}

func (p *ICMPProber) Accept(target string) (string, error) {
	var hostname string
	
	// Check if it's legacy format (icmpv4:host or icmpv6:host)
	if strings.HasPrefix(target, string(p.version)+":") {
		hostname = strings.TrimPrefix(target, string(p.version)+":")
	} else if p.version == ICMPV4 && isPlainHostname(target) {
		// For ICMPv4, accept plain hostnames/IPs
		hostname = target
	} else {
		return "", ErrNotAccepted
	}
	
	// Determine resolver type based on ICMP version
	resolvType := "ip4"
	if p.version == ICMPV6 {
		resolvType = "ip6"
	}
	
	// Resolve hostname to IP address
	ip, err := net.ResolveIPAddr(resolvType, hostname)
	if err != nil {
		return "", fmt.Errorf("failed to resolve '%s': %w", hostname, err)
	}
	
	// Check for duplicate IP addresses
	ipStr := ip.String()
	for _, existingIP := range p.targets {
		if existingIP.String() == ipStr {
			return p.targetMap[ipStr], nil // Return existing display name
		}
	}
	
	// Generate display name for new target
	displayName := ip.String()
	if net.ParseIP(hostname) == nil {
		displayName = fmt.Sprintf("%s(%s)", hostname, ip.String())
	}
	
	// Store target and mapping
	p.targets = append(p.targets, ip)
	p.targetMap[ipStr] = displayName
	
	return displayName, nil
}

// isPlainHostname checks if the target is a plain hostname/IP without protocol prefix
func isPlainHostname(target string) bool {
	// Not a URL scheme and not legacy format
	return !strings.Contains(target, "://") && !strings.Contains(target, ":")
}

func (p *ICMPProber) HasTargets() bool {
	return len(p.targets) > 0
}

func (p *ICMPProber) addTable(runCnt int, sentTime time.Time) {
	rt := runTime{runCnt: runCnt, sentTime: sentTime}
	addrMap := make(map[string]bool, len(p.targets))
	for _, t := range p.targets {
		addrMap[t.String()] = false
	}
	p.mu.Lock()
	p.tables[rt] = addrMap
	p.mu.Unlock()
}

func (p *ICMPProber) sent(r chan *Event, addr string) {
	r <- &Event{
		Target: addr,
		Result: SENT,
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
		r <- &Event{
			Target:   addr,
			Result:   SUCCESS,
			SentTime: k.sentTime,
			Rtt:      elapse,
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
		r <- &Event{
			Target:   addr,
			Result:   FAILED,
			SentTime: k.sentTime,
			Rtt:      0,
			Message:  err.Error(),
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
				r <- &Event{
					Target:   t,
					Result:   TIMEOUT,
					SentTime: rt.sentTime,
					Rtt:      p.timeout,
					Message:  "timeout",
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
	for _, t := range p.targets {
		_, err := p.c.WriteTo(b, t)
		p.sent(r, t.String())  // Use IP as event key
		if err != nil {
			p.failed(r, p.runCnt, t.String(), err)
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
