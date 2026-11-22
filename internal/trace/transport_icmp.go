package trace

import (
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/servak/mping/internal/icmpid"
	"github.com/servak/mping/internal/icmpsock"
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
	// hidesHops reports whether only the destination can be seen, because
	// the socket does not receive ICMP errors
	hidesHops() bool
}

// icmpTransport traces with ICMP echo requests, like mtr's default mode. The
// echo ID is fixed per tracer, so every probe is one flow for ECMP hashing.
// It comes from the process-wide allocator in internal/icmpid, shared with
// the ICMP probers, so that no two live tracers or probers of the process
// use the same ID (raw sockets see every echo reply on the host); it is
// released when the transport is closed, as tracers restart on every target
// change.
//
// The replies are echo replies from the destination and the ICMP errors the
// probes trigger (Time Exceeded, Destination Unreachable). They are received
// from
//
//   - a datagram ICMP socket and, on Linux, its error queue (unprivileged), or
//   - a raw ICMP socket (root / cap_net_raw)
type icmpTransport struct {
	v6       bool
	raw      bool
	errQueue bool // ICMP errors are on the error queue of conn
	destOnly bool // ICMP errors are not received at all
	conn     icmpConn
	dstAddr  net.Addr
	id       uint16
	lease    *icmpid.Lease
	checkID  bool

	writeMu sync.Mutex // TTL is a socket option, so set+write must be atomic
}

// icmpConn is a datagram or raw ICMP socket. Tests replace it.
type icmpConn interface {
	ReadFrom(b []byte) (int, net.Addr, error)
	WriteTo(b []byte, dst net.Addr) (int, error)
	LocalAddr() net.Addr
	Close() error
	setTTL(ttl int) error
	// enableRecvErr and receiveQueues: see the functions of these names
	enableRecvErr() error
	receiveQueues(onErr func(queuedError), onData func(b []byte, from string, at time.Time))
}

// listenFunc opens an ICMP socket like icmp.ListenPacket.
type listenFunc func(network, address string) (icmpConn, error)

// netConn is the icmpConn of a real socket.
type netConn struct {
	*icmp.PacketConn
}

func listenICMP(network, address string) (icmpConn, error) {
	c, err := icmp.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	return netConn{c}, nil
}

func (c netConn) v6() bool { return c.IPv6PacketConn() != nil }

func (c netConn) setTTL(ttl int) error {
	if c.v6() {
		return c.IPv6PacketConn().SetHopLimit(ttl)
	}
	return c.IPv4PacketConn().SetTTL(ttl)
}

// socket returns the socket itself, which icmp.PacketConn does not expose.
func (c netConn) socket() (syscall.Conn, error) {
	var pc net.PacketConn
	if c.v6() {
		pc = c.IPv6PacketConn().PacketConn
	} else {
		pc = c.IPv4PacketConn().PacketConn
	}
	if sc, ok := pc.(syscall.Conn); ok {
		return sc, nil
	}
	return nil, fmt.Errorf("no access to the socket of a %T", pc)
}

func (c netConn) enableRecvErr() error {
	sc, err := c.socket()
	if err != nil {
		return err
	}
	return enableRecvErr(sc, c.v6())
}

func (c netConn) receiveQueues(onErr func(queuedError), onData func([]byte, string, time.Time)) {
	if sc, err := c.socket(); err == nil {
		receiveQueues(sc, onErr, onData)
	}
}

// echoIDs allocates the echo IDs; tests replace it by a local allocator.
var echoIDs = icmpid.Default()

func newICMPTransport(dst *net.IPAddr, v6 bool) (*icmpTransport, error) {
	return openICMPTransport(runtime.GOOS, dst, v6, listenICMP)
}

// openICMPTransport opens the socket of an ICMP trace.
//
// On Linux that is a datagram socket with the ICMP errors on its error
// queue, or a raw one where datagram ICMP is not permitted or does not exist.
// Nothing is lost without privileges: the kernel queues every Time Exceeded
// and Destination Unreachable that quotes one of our probes, with the sender
// and the quoted echo header, which is all that is used of the quote. (What
// only a raw socket sees is not about a probe, or is broken: a failed
// reassembly, undefined codes, a wrong ICMP checksum.) Should there be no
// error queue, a datagram socket sees the destination only, and raw is
// preferred.
//
// On macOS raw is preferred, although the kernel hands datagram sockets the
// same ICMP messages as raw ones (that the IPv6 Time Exceeded arrives is not
// verified on a real path): only root can open a raw socket there, everyone
// else gets the datagram socket.
func openICMPTransport(goos string, dst *net.IPAddr, v6 bool, listen listenFunc) (*icmpTransport, error) {
	rawNet, dgramNet, laddr := "ip4:icmp", "udp4", "0.0.0.0"
	if v6 {
		rawNet, dgramNet, laddr = "ip6:ipv6-icmp", "udp6", "::"
	}
	openRaw := func() (*icmpTransport, error) {
		c, err := listen(rawNet, laddr)
		if err != nil {
			return nil, err
		}
		lease, err := icmpsock.AcquireID(c, echoIDs)
		if err != nil {
			return nil, err
		}
		return &icmpTransport{
			v6: v6, conn: c, id: lease.ID(), lease: lease, raw: true, checkID: true,
			dstAddr: &net.IPAddr{IP: dst.IP, Zone: dst.Zone},
		}, nil
	}
	openDatagram := func() (*icmpTransport, error) {
		c, lease, err := icmpsock.OpenDatagram(goos, echoIDs, func() (icmpConn, error) {
			return listen(dgramNet, laddr)
		})
		if err != nil {
			return nil, err
		}
		t := &icmpTransport{
			v6: v6, conn: c, id: lease.ID(), lease: lease, checkID: icmpsock.DatagramKeepsEchoID(goos),
			dstAddr: &net.UDPAddr{IP: dst.IP, Zone: dst.Zone},
		}
		if goos == "linux" {
			t.errQueue = c.enableRecvErr() == nil
			t.destOnly = !t.errQueue
		}
		return t, nil
	}

	var dgramErr, rawErr error
	var t *icmpTransport
	switch {
	case !icmpsock.DatagramSupported(goos):
		if t, rawErr = openRaw(); rawErr == nil {
			return t, nil
		}
		return nil, fmt.Errorf("cannot open a raw ICMP socket: %w%s", rawErr, privilegeHint(goos, rawErr))
	case goos == "linux":
		if t, dgramErr = openDatagram(); dgramErr == nil {
			if t.destOnly {
				if r, err := openRaw(); err == nil {
					_ = t.close()
					return r, nil
				}
			}
			return t, nil
		}
		if !icmpsock.DatagramUnavailable(dgramErr) { // raw sockets would fail too
			return nil, fmt.Errorf("cannot open a datagram ICMP socket: %w", dgramErr)
		}
		if t, rawErr = openRaw(); rawErr == nil {
			return t, nil
		}
	default:
		if t, rawErr = openRaw(); rawErr == nil {
			return t, nil
		}
		if t, dgramErr = openDatagram(); dgramErr == nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("cannot open an ICMP socket: datagram: %w; raw: %w%s", dgramErr, rawErr, privilegeHint(goos, rawErr))
}

// privilegeHint says how to get permission if that is what err is about.
func privilegeHint(goos string, err error) string {
	if !icmpsock.Denied(err) {
		return ""
	}
	return icmpsock.PrivilegeHint(goos)
}

func (t *icmpTransport) privileged() bool { return t.raw }

func (t *icmpTransport) hidesHops() bool { return t.destOnly }

func (t *icmpTransport) close() error {
	err := t.conn.Close()
	t.lease.Release()
	return err
}

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
	if err := t.conn.setTTL(ttl); err != nil {
		return err
	}
	write := func() error {
		_, err := t.conn.WriteTo(b, t.dstAddr)
		return err
	}
	if t.errQueue {
		return retrySend(write)
	}
	return write()
}

func (t *icmpTransport) receive(deliver func(reply, string, time.Time)) {
	onData := func(b []byte, from string, at time.Time) {
		if r, err := parseReply(t.v6, b); err == nil && t.accept(r) {
			deliver(r, from, at)
		}
	}
	if t.errQueue {
		onErr := func(e queuedError) {
			if r, from, err := icmpErrQueueReply(t.v6, e); err == nil && t.accept(r) {
				deliver(r, from, e.at)
			}
		}
		t.conn.receiveQueues(onErr, onData)
		return
	}
	buf := make([]byte, maxPacketSize)
	for {
		n, peer, err := t.conn.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		onData(buf[:n], peerIP(peer), time.Now())
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
