package prober

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/servak/mping/internal/icmpid"
)

const (
	ICMPV4 ProbeType = "icmpv4"
	ICMPV6 ProbeType = "icmpv6"
)

type (
	ICMPProber struct {
		version ProbeType
		prefix  string // Custom prefix like "my-ping", "icmpv4", etc.
		c       packetConn
		body    []byte
		targets map[string]icmpTarget // IPAddr string -> event key and name
		timeout time.Duration
		runCnt  int
		runID   int                         // echo ID, unique per prober in the process
		lease   *icmpid.Lease               // owns runID until the socket is closed
		tables  map[runTime]map[string]bool // guarded by mu
		mu      sync.Mutex
		loop    *runLoop
		// wake is signalled when a round is fully answered, so that the
		// drain after Stop does not wait for its timeout
		wake chan struct{}

		closeOnce sync.Once
	}

	ICMPConfig struct {
		Body            string `yaml:"body"`
		TOS             int    `yaml:"tos,omitempty"`
		TTL             int    `yaml:"ttl,omitempty"`
		SourceInterface string `yaml:"source_interface,omitempty"`
	}

	// icmpTarget is the event key and display name of a target address
	icmpTarget struct {
		key  string
		name string
	}

	runTime struct {
		runCnt   int
		sentTime time.Time
	}

	// packetConn is the part of *icmp.PacketConn the prober uses
	packetConn interface {
		ReadFrom(b []byte) (int, net.Addr, error)
		WriteTo(b []byte, dst net.Addr) (int, error)
		Close() error
	}
)

// Validate validates the ICMP configuration
func (cfg *ICMPConfig) Validate() error {
	// Basic validation - could be extended with more checks
	if cfg.TOS < 0 || cfg.TOS > 255 {
		return fmt.Errorf("invalid TOS value: %d (must be 0-255)", cfg.TOS)
	}
	if cfg.TTL < 0 || cfg.TTL > 255 {
		return fmt.Errorf("invalid TTL value: %d (must be 0-255)", cfg.TTL)
	}
	return nil
}

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
		if err != nil {
			return nil, err
		}
	}
	lease, err := icmpid.Acquire()
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("cannot allocate ICMP echo ID: %w", err)
	}
	return newICMPProber(t, c, []byte(cfg.Body), prefix, lease), nil
}

// newICMPProber returns a prober on c with its own echo ID. The echo ID
// tells the replies of this prober apart from those of the other
// probers and path tracers of the process, which ping with overlapping
// sequence numbers: a raw ICMP socket receives the echo replies of every
// socket on the host. IDs come from the process-wide allocator in
// internal/icmpid, shared with internal/trace, and are released when the
// socket is closed.
//
// The probers only use raw sockets. Datagram ICMP sockets (Linux
// "ping" sockets) would need different matching: the kernel rewrites the
// echo ID to the socket's port and delivers each socket only its own
// replies, so the ID check in recvPkts would have to be skipped there.
func newICMPProber(t ProbeType, c packetConn, body []byte, prefix string, lease *icmpid.Lease) *ICMPProber {
	return &ICMPProber{
		version: t,
		prefix:  prefix,
		c:       c,
		tables:  make(map[runTime]map[string]bool),
		targets: make(map[string]icmpTarget),
		runID:   int(lease.ID()),
		lease:   lease,
		runCnt:  0,
		body:    body,
		loop:    newRunLoop(),
		wake:    make(chan struct{}, 1),
	}
}

// isBuiltin reports whether p is one of the built-in icmpv4/icmpv6 probers,
// whose targets are keyed and shown by their bare address.
func (p *ICMPProber) isBuiltin() bool {
	return p.prefix == string(p.version)
}

// Accept adds a target written with the prober's scheme ("my-ping://host",
// "icmpv4://host" or the legacy "icmpv4:host"). The targets of a custom
// prober are keyed and shown with its scheme.
func (p *ICMPProber) Accept(target string) error {
	return p.accept(target, false)
}

// acceptPlain adds a target the user wrote without a scheme, routed here
// because p is the default prober. Like the targets of the built-in
// probers, it is shown by its bare address whichever prober is the
// default; only its key carries the scheme of a custom prober, so that it
// does not merge with the same address probed by another prober.
func (p *ICMPProber) acceptPlain(target string) error {
	return p.accept(target, true)
}

func (p *ICMPProber) accept(target string, plain bool) error {
	hostname, ok := cutPrefix(target, p.prefix)
	if !ok {
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
	if _, exists := p.targets[ipStr]; exists {
		return nil // Already exists, no need to add again
	}

	// The built-in probers key their targets by address. Any other ICMP
	// prober (e.g. one with TTL=1) may probe the same address, so its key
	// carries its own prefix to keep the two apart in the stats, and so
	// does its display name for targets written with its scheme.
	key, displayName := ipStr, icmpDisplayName(hostname, ipStr, "")
	if !p.isBuiltin() {
		key = icmpDisplayName(ipStr, ipStr, p.prefix)
		if !plain {
			displayName = icmpDisplayName(hostname, ipStr, p.prefix)
		}
	}
	p.targets[ipStr] = icmpTarget{key: key, name: displayName}

	return nil
}

// icmpDisplayName formats a target as its address, or "host(address)" when
// it was given by name, behind "prefix://" when prefix is set. A prefixed
// bare IPv6 address is bracketed (my-v6://[2001:db8::1]) so that it parses
// as a URL host rather than as a host and port.
func icmpDisplayName(hostname, ipStr, prefix string) string {
	name := ipStr
	if net.ParseIP(hostname) == nil && hostname != ipStr {
		name = fmt.Sprintf("%s(%s)", hostname, ipStr)
	}
	if prefix == "" {
		return name
	}
	if name == ipStr && strings.Contains(ipStr, ":") {
		name = "[" + ipStr + "]"
	}
	return prefix + "://" + name
}

func (p *ICMPProber) addTable(runCnt int, sentTime time.Time) {
	rt := runTime{runCnt: runCnt, sentTime: sentTime}
	addrMap := make(map[string]bool, len(p.targets))
	for ipStr := range p.targets {
		addrMap[ipStr] = false
	}
	p.mu.Lock()
	p.tables[rt] = addrMap
	p.mu.Unlock()
}

// getTargetInfo returns Key and DisplayName for the given IP address
func (p *ICMPProber) getTargetInfo(addr string) (string, string) {
	if t, exists := p.targets[addr]; exists {
		return t.key, t.name
	}
	return addr, addr // fallback
}

// answered drops round rt once every target has answered, waking a drain
// that waits for it. p.mu must be held.
func (p *ICMPProber) answered(rt runTime, table map[string]bool) {
	for _, done := range table {
		if !done {
			return
		}
	}
	delete(p.tables, rt)
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// success marks the reply from addr to round runCnt and returns its event,
// or nil for a duplicate or unexpected reply. Events are returned rather than
// sent so that p.mu is never held while blocking on the event channel.
func (p *ICMPProber) success(runCnt int, addr string, payload icmp.Message, packetData []byte, packetSize int) *Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; !ok {
			return nil
		}
		if table[addr] {
			continue
		}
		table[addr] = true
		p.answered(k, table)
		elapse := time.Since(k.sentTime)
		key, displayName := p.getTargetInfo(addr)
		if elapse > p.timeout {
			// Too late: this is the round's TIMEOUT, which checkTimeout
			// will not report again
			return failureEvent(key, displayName, TIMEOUT, k.sentTime, p.timeout, "timeout")
		}
		details := &ProbeDetails{
			ProbeType: string(p.version),
			ICMP:      p.extractICMPDetails(runCnt, addr, payload, packetData, packetSize),
		}
		return successEvent(key, displayName, k.sentTime, elapse, details)
	}
	return nil
}

// extractICMPDetails extracts detailed information from ICMP packet
func (p *ICMPProber) extractICMPDetails(runCnt int, addr string, payload icmp.Message, packetData []byte, packetSize int) *ICMPDetails {
	var payloadContent string
	var checksum uint16

	// Extract echo data if available
	if echoBody, ok := payload.Body.(*icmp.Echo); ok {

		// Format payload content with length limit
		payloadContent = formatPayloadContent(echoBody.Data)
	}

	// Extract checksum from raw packet data if available
	// ICMP checksum is at offset 2-3 in the ICMP header
	if len(packetData) >= 4 {
		checksum = binary.BigEndian.Uint16(packetData[2:4])
	}

	// Convert ICMP type to int safely
	var icmpType int
	switch payload.Type {
	case ipv4.ICMPTypeEchoReply:
		icmpType = 0
	case ipv6.ICMPTypeEchoReply:
		icmpType = 129
	default:
		// Use reflection or type assertion for other types
		if t, ok := payload.Type.(interface{ Int() int }); ok {
			icmpType = t.Int()
		} else {
			icmpType = -1 // Unknown type
		}
	}

	details := &ICMPDetails{
		Sequence:   runCnt,
		PacketSize: packetSize,
		ICMPType:   icmpType,
		ICMPCode:   payload.Code,
		Checksum:   checksum,
		Payload:    payloadContent,
	}

	return details
}

// formatPayloadContent formats payload bytes for display with length limit
func formatPayloadContent(data []byte) string {
	const maxDisplayLength = 32 // Maximum characters to display

	if len(data) == 0 {
		return ""
	}

	// Convert to string, replacing non-printable characters
	var result strings.Builder
	for _, b := range data {
		if b >= 32 && b <= 126 { // Printable ASCII characters
			result.WriteByte(b)
		} else {
			fmt.Fprintf(&result, "\\x%02x", b)
		}
	}

	payloadStr := result.String()

	// Truncate if too long
	if len(payloadStr) > maxDisplayLength {
		payloadStr = payloadStr[:maxDisplayLength-3] + "..."
	}

	return payloadStr
}

// failed marks addr as answered in round runCnt and returns its FAILED event
// (nil when the round is unknown).
func (p *ICMPProber) failed(runCnt int, addr string, err error) *Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; ok {
			table[addr] = true
			p.answered(k, table)
		}
		key, displayName := p.getTargetInfo(addr)
		return failureEvent(key, displayName, FAILED, k.sentTime, 0, err.Error())
	}
	return nil
}

// checkTimeout drops the rounds older than the timeout and returns TIMEOUT
// events for their unanswered targets, the number of rounds still waiting
// for replies and when the oldest of those times out (zero if none).
func (p *ICMPProber) checkTimeout() (events []*Event, pending int, next time.Time) {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for rt, table := range p.tables {
		d := rt.sentTime.Add(p.timeout)
		if d.After(now) {
			if next.IsZero() || d.Before(next) {
				next = d
			}
			continue
		}
		for t, res := range table {
			if !res {
				key, displayName := p.getTargetInfo(t)
				events = append(events, failureEvent(key, displayName, TIMEOUT, rt.sentTime, p.timeout, "timeout"))
			}
		}
		delete(p.tables, rt)
	}
	return events, len(p.tables), next
}

// expire reports the rounds that timed out and returns the number of rounds
// still outstanding. It arms deadline for the next timeout, if any.
func (p *ICMPProber) expire(r chan *Event, deadline *time.Timer) int {
	events, pending, next := p.checkTimeout()
	emit(r, events...)
	if !next.IsZero() {
		deadline.Reset(time.Until(next))
	}
	return pending
}

// emit sends the events that were collected under p.mu.
func emit(r chan<- *Event, events ...*Event) {
	for _, e := range events {
		if e != nil {
			r <- e
		}
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

	b, marshalErr := m.Marshal(nil)
	n := time.Now()
	p.addTable(p.runCnt, n)
	for ipStr := range p.targets {
		if marshalErr != nil {
			// Not expected for an echo request; fail the round instead of exiting
			emit(r, p.failed(p.runCnt, ipStr, fmt.Errorf("failed ICMP encode: %w", marshalErr)))
			continue
		}
		ip, err := ipAddr(ipStr)
		if err != nil {
			emit(r, p.failed(p.runCnt, ipStr, err))
			continue
		}
		_, err = p.c.WriteTo(b, ip)
		key, displayName := p.getTargetInfo(ipStr)
		r <- sentEvent(key, displayName, n)
		if err != nil {
			emit(r, p.failed(p.runCnt, ipStr, err))
		}
	}
}

// ipAddr converts a target key (an IP address from Accept) to the address
// to send to. Only zoned IPv6 addresses (fe80::1%eth0) need the resolver.
func ipAddr(s string) (*net.IPAddr, error) {
	if ip := net.ParseIP(s); ip != nil {
		return &net.IPAddr{IP: ip}, nil
	}
	return net.ResolveIPAddr("ip", s)
}

// recvPkts reads replies until the connection is closed.
func (p *ICMPProber) recvPkts(r chan *Event) {
	proto := ipv4.ICMPTypeEchoReply.Protocol()
	if p.version == ICMPV6 {
		proto = ipv6.ICMPTypeEchoReply.Protocol()
	}
	pktbuf := make([]byte, maxPacketSize)
	for {
		n, addr, err := p.c.ReadFrom(pktbuf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return // closed by Start or Stop
			}
			// Unexpected read error: keep receiving, but do not spin
			time.Sleep(10 * time.Millisecond)
			continue
		}
		rm, err := icmp.ParseMessage(proto, pktbuf[:n])
		if err != nil || n < 8 {
			continue // not a packet of ours; skip it
		}
		id := binary.BigEndian.Uint16(pktbuf[4:6])
		if id != uint16(p.runID) {
			continue
		}
		seq := binary.BigEndian.Uint16(pktbuf[6:8])
		if rm.Code == 0 {
			switch rm.Type {
			case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
				emit(r, p.success(int(seq), addr.String(), *rm, pktbuf[:n], n))
			}
		}
	}
}

func (p *ICMPProber) closeConn() {
	p.closeOnce.Do(func() {
		_ = p.c.Close()
		p.lease.Release()
	})
}

// Start sends one echo request per target and round until Stop. Requests
// are reported as TIMEOUT as soon as their timeout expires. After Stop it
// keeps receiving until every outstanding round is answered or has timed
// out, then closes the socket and returns once the receiver has exited.
func (p *ICMPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	ok, err := p.loop.begin()
	if !ok {
		return err
	}
	defer p.loop.finish()

	for _, ipStr := range slices.Sorted(maps.Keys(p.targets)) {
		t := p.targets[ipStr]
		register(r, t.key, t.name, p.version)
	}
	p.timeout = timeout

	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		p.recvPkts(r)
	}()
	defer func() {
		p.closeConn()
		<-recvDone
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	p.probe(r)
	p.expire(r, deadline)
loop:
	for {
		select {
		case <-p.loop.stopping():
			break loop
		case <-ticker.C:
			p.probe(r)
			p.expire(r, deadline)
		case <-deadline.C:
			p.expire(r, deadline)
		}
	}
	// Wait for the outstanding rounds: until each is answered (wake) or
	// times out (deadline), never longer than the timeout
	for p.expire(r, deadline) > 0 {
		select {
		case <-deadline.C:
		case <-p.wake:
		}
	}
	return nil
}

// Stop stops probing and waits for Start to return, which includes waiting
// for the replies of the last rounds. It is safe to call more than once; if
// Start never ran, it closes the socket itself.
func (p *ICMPProber) Stop() {
	if !p.loop.stop() {
		p.closeConn()
	}
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

	// Otherwise it is an interface name. IPv6 skips loopback addresses
	// (::1); IPv4 accepts any address, including 127.0.0.1 on "lo". A
	// link-local IPv6 address comes with its zone (fe80::1%en0).
	v6 := probeType == ICMPV6
	addr, err := interfaceIP(sourceInterface, v6, v6)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}
