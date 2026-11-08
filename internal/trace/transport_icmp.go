package trace

import (
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// transport sends probes and delivers the matching ICMP replies. The probe
// key identifies a probe in the tracer's pending table: the echo sequence for
// ICMP, the destination port for UDP.
type transport interface {
	// send sends a probe with the given TTL / hop limit
	send(ttl int, key uint16) error
	// receive delivers replies to our probes until the transport is closed
	receive(deliver func(r reply, from string, at time.Time))
	close() error
	// privileged reports whether a raw socket is used
	privileged() bool
}

// Echo IDs are a random per-process base plus a counter: unique per tracer
// within the process, and unrelated to the pid, so that they do not follow
// the pid&0xffff IDs of the ICMP probers of this or another mping process
// (pids are often consecutive).
var (
	idBase    = uint16(rand.Uint32())
	idCounter atomic.Uint32
)

// nextEchoID returns the echo ID for a new tracer, never pid&0xffff (the ID
// of this process' ICMP prober)
func nextEchoID() uint16 {
	return echoID(idBase, idCounter.Add(1), os.Getpid())
}

func echoID(base uint16, n uint32, pid int) uint16 {
	id := base + uint16(n)
	if id == uint16(pid&0xffff) {
		id++
	}
	return id
}

// icmpTransport traces with ICMP echo requests, like mtr's default mode. The
// echo ID is fixed per tracer, so every probe is one flow for ECMP hashing.
type icmpTransport struct {
	v6      bool
	raw     bool
	conn    *icmp.PacketConn
	dstAddr net.Addr
	id      uint16
	checkID bool

	writeMu sync.Mutex // TTL is a socket option, so set+write must be atomic
}

// newICMPTransport prefers a raw socket and falls back to an unprivileged
// datagram ICMP socket; on macOS the fallback still receives Time Exceeded
// messages, on Linux only the destination may be visible without privileges.
func newICMPTransport(dst *net.IPAddr, v6 bool) (*icmpTransport, error) {
	t := &icmpTransport{v6: v6, id: nextEchoID()}
	rawNet, dgramNet, laddr := "ip4:icmp", "udp4", "0.0.0.0"
	if v6 {
		rawNet, dgramNet, laddr = "ip6:ipv6-icmp", "udp6", "::"
	}
	if c, err := icmp.ListenPacket(rawNet, laddr); err == nil {
		t.conn, t.raw = c, true
		t.dstAddr = &net.IPAddr{IP: dst.IP, Zone: dst.Zone}
		t.checkID = true
	} else if c, err2 := icmp.ListenPacket(dgramNet, laddr); err2 == nil {
		t.conn = c
		t.dstAddr = &net.UDPAddr{IP: dst.IP, Zone: dst.Zone}
		// Linux rewrites the echo ID of datagram ICMP sockets and demuxes
		// replies per socket, so only check the ID elsewhere (e.g. macOS).
		t.checkID = runtime.GOOS != "linux"
	} else {
		return nil, fmt.Errorf("cannot open ICMP socket (%v; %v): run as root or grant cap_net_raw", err, err2)
	}
	return t, nil
}

func (t *icmpTransport) privileged() bool { return t.raw }

func (t *icmpTransport) close() error { return t.conn.Close() }

func (t *icmpTransport) send(ttl int, seq uint16) error {
	msg := icmp.Message{Body: &icmp.Echo{ID: int(t.id), Seq: int(seq), Data: []byte("mping-trace")}}
	if t.v6 {
		msg.Type = ipv6.ICMPTypeEchoRequest
	} else {
		msg.Type = ipv4.ICMPTypeEcho
	}
	b, err := msg.Marshal(nil)
	if err != nil {
		return err
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.v6 {
		err = t.conn.IPv6PacketConn().SetHopLimit(ttl)
	} else {
		err = t.conn.IPv4PacketConn().SetTTL(ttl)
	}
	if err != nil {
		return err
	}
	_, err = t.conn.WriteTo(b, t.dstAddr)
	return err
}

func (t *icmpTransport) receive(deliver func(reply, string, time.Time)) {
	buf := make([]byte, maxPacketSize)
	for {
		n, peer, err := t.conn.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		now := time.Now()
		r, err := parseReply(t.v6, buf[:n])
		if err != nil || !t.accept(r) {
			continue
		}
		deliver(r, peerIP(peer), now)
	}
}

// accept reports whether r answers one of our probes: the echo ID matches,
// where the kernel keeps it; elsewhere (Linux datagram sockets) the kernel
// has already matched the reply to our socket by its own ID. The tracer then
// matches the sequence to a pending probe. Addresses are not checked: DNAT
// and load balancers answer echo requests from another address, and often
// leave the destination of the quoted datagram unrewritten.
func (t *icmpTransport) accept(r reply) bool {
	return !t.checkID || r.id == t.id
}

// peerIP returns the address of a reply's sender
func peerIP(a net.Addr) string {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP.String()
	case *net.UDPAddr:
		return v.IP.String()
	}
	return a.String()
}
