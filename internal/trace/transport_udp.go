package trace

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// UDPBasePort is the first destination port of UDP probes (the
	// traditional traceroute port).
	UDPBasePort = 33434
	// UDPPortRange is how many destination ports (flows) UDP probes rotate
	// through: UDPBasePort .. UDPBasePort+UDPPortRange-1.
	UDPPortRange = 64
	// udpRoundStride shifts the ports of consecutive rounds so that a round
	// (at most maxHops < udpRoundStride ports) never reuses a port of the
	// previous round, while every hop still cycles through all ports
	// (udpRoundStride and UDPPortRange are coprime).
	udpRoundStride = 33

	// sendRetries is how often a send is retried when the socket reports an
	// earlier ICMP error instead of sending (Linux with IP_RECVERR)
	sendRetries = 3
)

var udpPayload = []byte("mping-trace")

// udpPort returns the destination port of the probe for ttl in round:
//
//	UDPBasePort + (udpRoundStride*round + ttl) mod UDPPortRange
//
// Routers hash ECMP on the L4 ports, so rotating the port makes successive
// probes to one TTL take every branch the probed service's own traffic
// (a new source port per connection) may take. Ports repeat every
// UDPPortRange rounds, so per-branch statistics accumulate.
func udpPort(round, ttl int) uint16 {
	return uint16(UDPBasePort + (udpRoundStride*round+ttl)%UDPPortRange)
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
	conn    *net.UDPConn
	srcPort uint16
	icmp    *icmp.PacketConn // nil: receive from the error queue
	raw     bool

	writeMu sync.Mutex // TTL is a socket option, so set+write must be atomic
}

func newUDPTransport(dst net.IP, v6 bool) (*udpTransport, error) {
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
		dst:     dst,
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
	dst := &net.UDPAddr{IP: t.dst, Port: int(port)}
	for i := 0; ; i++ {
		_, err = t.conn.WriteToUDP(udpPayload, dst)
		// With IP_RECVERR Linux also reports a received ICMP error once
		// as the result of the next socket call. The error itself is
		// still queued for receive, so just send again.
		if err == nil || i >= sendRetries || !isICMPErrno(err) {
			return err
		}
	}
}

// isICMPErrno reports whether err is the errno Linux derives from an ICMP
// error (Time Exceeded, Port / Host / Net Unreachable)
func isICMPErrno(err error) bool {
	for _, e := range []syscall.Errno{syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EPROTO} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
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
		if err != nil || r.id != t.srcPort {
			continue // not a reply to our probes
		}
		deliver(r, peerIP(peer), now)
	}
}
