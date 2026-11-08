package trace

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// UDPBasePort is the first destination port of UDP probes (the
	// traditional traceroute port).
	UDPBasePort = 33434
	// udpRoundStride shifts the ports of consecutive rounds by more than a
	// round (maxHops ports), so that rounds do not overlap; see udpPort.
	// It is odd (prime) so that the ports of one TTL in successive rounds
	// also differ in their low bits, which simple ECMP hashes look at.
	udpRoundStride = maxHops + 1
	// maxUDPPortRange is the most destination ports (flows) UDP probes
	// rotate through: every port from UDPBasePort up to 65535. It is not a
	// multiple of udpRoundStride, see udpPortRange.
	maxUDPPortRange = 65536 - UDPBasePort

	// sendRetries is how often a send is retried when the socket reports an
	// earlier ICMP error instead of sending (Linux with IP_RECVERR)
	sendRetries = 3
)

var udpPayload = []byte("mping-trace")

// udpProbePayload returns the payload of the probe for ttl: udpPayload
// followed by ttl bytes of value ttl. The TTL is thus in the UDP length of
// the datagram quoted by ICMP errors and in the payload the Linux error
// queue returns, so a reply can be checked against the probe pending under
// its destination port (see udpReplyTTL).
func udpProbePayload(ttl int) []byte {
	b := make([]byte, len(udpPayload)+ttl)
	copy(b, udpPayload)
	for i := len(udpPayload); i < len(b); i++ {
		b[i] = byte(ttl)
	}
	return b
}

// udpPortRange returns how many destination ports UDP probes rotate through
// for the given round interval and probe timeout. A probe may still be
// pending for up to w = ceil(timeout/interval)+1 rounds (with one round of
// slack). Ports of probes d rounds apart differ by udpRoundStride*d ± (less
// than maxHops), so (w+1)*udpRoundStride ports guarantee that no probe of
// the last w rounds shares a port with one of this round, and a late reply
// is never credited to another TTL. The range is coprime with the stride so
// that every TTL cycles through all ports. Past maxUDPPortRange (a timeout
// of more than about 1000 intervals) the tracer counts probes older than
// udpPortWindow rounds as lost instead, which keeps the guarantee.
func udpPortRange(interval, timeout time.Duration) int {
	w := 2
	if interval > 0 && timeout > 0 {
		w = int(min((timeout+interval-1)/interval, maxUDPPortRange)) + 1
	}
	n := min((w+1)*udpRoundStride, maxUDPPortRange)
	for gcd(n, udpRoundStride) != 1 {
		n++
	}
	return n
}

// udpPortWindow returns how many rounds back from the current one no probe
// shares a port with it, for the given number of ports: the largest w with
// udpRoundStride*w + maxHops-1 < ports (see udpPortRange).
func udpPortWindow(ports int) int {
	return (ports - maxHops) / udpRoundStride
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// udpPort returns the destination port of the probe for ttl in round, with
// ports UDP probes rotate through (udpPortRange):
//
//	UDPBasePort + (udpRoundStride*round + ttl - 1) mod ports
//
// Routers hash ECMP on the L4 ports, so rotating the port makes successive
// probes to one TTL take every branch the probed service's own traffic
// (a new source port per connection) may take. Ports repeat every `ports`
// rounds, so per-branch statistics accumulate.
func udpPort(ports, round, ttl int) uint16 {
	return uint16(UDPBasePort + (udpRoundStride*round+ttl-1)%ports) // round >= 0, ttl >= 1
}

// errUDPUnsupported is returned when ICMP errors for UDP probes cannot be
// received without privileges on this platform
var errUDPUnsupported = errors.New("UDP trace needs root (or cap_net_raw) on this platform")

// udpTransport traces with UDP datagrams, like the classic traceroute. It
// sends from one socket bound to an ephemeral port; that source port
// identifies our probes and the destination port identifies the probe.
// Replies are the ICMP errors the probes trigger: Time Exceeded from
// routers, Port Unreachable from the destination. They are received from
//
//   - a raw ICMP socket (root / cap_net_raw), or
//   - the UDP socket's error queue (Linux, IP_RECVERR, unprivileged), or
//   - a datagram ICMP socket (macOS, unprivileged)
type udpTransport struct {
	v6      bool
	dst     net.IP
	zone    string // IPv6 zone of dst (link-local destinations)
	conn    *net.UDPConn
	srcPort uint16
	icmp    *icmp.PacketConn // nil: receive from the error queue
	raw     bool

	writeMu sync.Mutex // TTL is a socket option, so set+write must be atomic
}

func newUDPTransport(dst *net.IPAddr, v6 bool) (*udpTransport, error) {
	udpNet, rawNet, dgramNet, laddr := "udp4", "ip4:icmp", "udp4", "0.0.0.0"
	if v6 {
		udpNet, rawNet, dgramNet, laddr = "udp6", "ip6:ipv6-icmp", "udp6", "::"
	}
	conn, err := net.ListenUDP(udpNet, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open UDP socket: %w", err)
	}
	t := &udpTransport{
		v6:      v6,
		dst:     dst.IP,
		zone:    dst.Zone,
		conn:    conn,
		srcPort: uint16(conn.LocalAddr().(*net.UDPAddr).Port),
	}

	rawErr := error(nil)
	if t.icmp, rawErr = icmp.ListenPacket(rawNet, laddr); rawErr == nil {
		t.raw = true
		return t, nil
	}
	errqErr := enableRecvErr(conn, v6)
	if errqErr == nil {
		return t, nil
	}
	// Datagram ICMP sockets only see ICMP errors of other sockets outside
	// Linux (which demuxes them per socket)
	if runtime.GOOS != "linux" {
		var dgramErr error
		if t.icmp, dgramErr = icmp.ListenPacket(dgramNet, laddr); dgramErr == nil {
			return t, nil
		}
	}
	conn.Close()
	return nil, fmt.Errorf("%w (%v)", errUDPUnsupported, rawErr)
}

func (t *udpTransport) privileged() bool { return t.raw }

func (t *udpTransport) close() error {
	err := t.conn.Close()
	if t.icmp != nil {
		if err2 := t.icmp.Close(); err == nil {
			err = err2
		}
	}
	return err
}

func (t *udpTransport) send(ttl int, port uint16) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	var err error
	if t.v6 {
		err = ipv6.NewPacketConn(t.conn).SetHopLimit(ttl)
	} else {
		err = ipv4.NewPacketConn(t.conn).SetTTL(ttl)
	}
	if err != nil {
		return err
	}
	dst := &net.UDPAddr{IP: t.dst, Port: int(port), Zone: t.zone}
	payload := udpProbePayload(ttl)
	for i := 0; ; i++ {
		_, err = t.conn.WriteToUDP(payload, dst)
		// With IP_RECVERR Linux also reports a received ICMP error once
		// as the result of the next socket call. The error itself is
		// still queued for receive, so just send again.
		if err == nil || i >= sendRetries || !isICMPErrno(err) {
			return err
		}
	}
}

func (t *udpTransport) receive(deliver func(reply, string, time.Time)) {
	if t.icmp == nil {
		t.receiveErrQueue(deliver)
		return
	}
	buf := make([]byte, maxPacketSize)
	for {
		n, peer, err := t.icmp.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		now := time.Now()
		r, err := parseUDPReply(t.v6, buf[:n])
		if err != nil || !t.accept(r) {
			continue // not a reply to our probes
		}
		deliver(r, peerIP(peer), now)
	}
}

// accept reports whether the ICMP error r quotes one of our probes: our
// source port, and a TTL that the tracer checks against the probe pending
// under the destination port. Load balancers and DNAT often leave the
// quoted destination unrewritten, so it is only checked when the TTL is
// unknown and the ports alone could match another socket's datagram.
func (t *udpTransport) accept(r reply) bool {
	return r.id == t.srcPort && (r.ttl != 0 || sameAddr(t.dst, r.dst))
}
