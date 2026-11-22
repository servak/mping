package prober

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"syscall"

	"golang.org/x/net/icmp"

	"github.com/servak/mping/internal/icmpid"
)

// socketType is the kind of ICMP socket a prober uses.
type socketType string

const (
	// datagramSocket is a "ping" socket, which needs no privileges: always
	// available on macOS, on Linux to the groups in
	// net.ipv4.ping_group_range (which governs IPv6 too).
	datagramSocket socketType = "datagram"
	// rawSocket needs root or CAP_NET_RAW (Administrator on Windows).
	rawSocket socketType = "raw"
)

// keepsEchoID reports whether replies on a socket of this type carry the
// echo ID we put in the request. Linux replaces the ID on datagram sockets
// with one of its own per socket and delivers to a socket only the replies
// to that ID, so there the ID must not, and need not, be checked. Everywhere
// else it must: raw sockets receive the replies to every socket on the host,
// and so do datagram sockets on macOS.
func (s socketType) keepsEchoID(goos string) bool {
	return s != datagramSocket || goos != "linux"
}

// datagramICMPSupported reports whether golang.org/x/net/icmp supports
// datagram ICMP on goos.
func datagramICMPSupported(goos string) bool {
	return goos == "linux" || goos == "darwin"
}

// icmpSocket is an open ICMP socket with the echo ID of its prober.
type icmpSocket struct {
	conn    packetConn // takes and returns *net.IPAddr on both types
	typ     socketType
	checkID bool          // replies carry the ID of lease, and only those are ours
	lease   *icmpid.Lease // owner of the echo ID until the socket is closed
}

// listenFunc opens an ICMP socket like icmp.ListenPacket. Tests replace it.
type listenFunc func(network, address string) (packetConn, error)

// listenICMP is the listenFunc of the real sockets: both socket types get
// the same options.
func listenICMP(t ProbeType, cfg *ICMPConfig) listenFunc {
	return func(network, address string) (packetConn, error) {
		c, err := icmp.ListenPacket(network, address)
		if err != nil {
			return nil, err
		}
		allowBroadcast(c)
		if p := c.IPv4PacketConn(); p != nil {
			if cfg.TOS != 0 {
				p.SetTOS(cfg.TOS)
			}
			if cfg.TTL != 0 {
				p.SetTTL(cfg.TTL)
			}
		}
		return newICMPConn(c, t), nil
	}
}

// openICMPSocket opens an ICMP socket bound to address, with the least
// privileges that will do: a datagram socket, or a raw one where datagram
// ICMP is not permitted or does not exist. Any other failure of the datagram
// socket (a source address this host does not have, no IPv6, no file
// descriptors) is one the raw socket would share, and is reported as it is.
func openICMPSocket(goos string, t ProbeType, address string, listen listenFunc) (*icmpSocket, error) {
	dgramNet, rawNet := "udp4", "ip4:icmp"
	if t == ICMPV6 {
		dgramNet, rawNet = "udp6", "ip6:ipv6-icmp"
	}

	var dgramErr error
	if datagramICMPSupported(goos) {
		var s *icmpSocket
		if s, dgramErr = openDatagram(goos, dgramNet, address, listen); dgramErr == nil {
			return s, nil
		}
		if !datagramUnavailable(dgramErr) {
			return nil, fmt.Errorf("cannot open a datagram ICMP socket on %s: %w%s", address, dgramErr, addressHint(dgramErr))
		}
	}

	c, err := listen(rawNet, address)
	if err == nil {
		lease, err := acquireEchoID(c)
		if err != nil {
			return nil, err
		}
		return &icmpSocket{conn: c, typ: rawSocket, checkID: rawSocket.keepsEchoID(goos), lease: lease}, nil
	}

	hint := addressHint(err)
	if dgramErr == nil { // raw sockets are all there is on goos
		if isDenied(err) {
			hint = privilegeHint(goos)
		}
		return nil, fmt.Errorf("cannot open a raw ICMP socket on %s: %w%s", address, err, hint)
	}
	if hint == "" {
		hint = privilegeHint(goos)
	}
	return nil, fmt.Errorf("cannot open an ICMP socket on %s: datagram: %w; raw: %w%s", address, dgramErr, err, hint)
}

// maxReserveAttempts limits how many sockets openDatagram opens to get a
// free echo ID.
const maxReserveAttempts = 3

// openDatagram opens a datagram ICMP socket and takes its echo ID.
//
// On Linux that is the ID the kernel chose, reserved so that no raw socket of
// this process gets the same one: path tracers run on raw sockets when they
// can, and the two would see each other's replies. Should a tracer hold the
// ID already (the kernel does not know about those), another socket gets
// another ID.
func openDatagram(goos, network, address string, listen listenFunc) (*icmpSocket, error) {
	checkID := datagramSocket.keepsEchoID(goos)
	for attempt := 1; ; attempt++ {
		c, err := listen(network, address)
		if err != nil {
			return nil, err
		}
		s := &icmpSocket{conn: datagramConn{c}, typ: datagramSocket, checkID: checkID}
		if id := kernelEchoID(c); !checkID && id != 0 {
			var ok bool
			if s.lease, ok = icmpid.Reserve(id); ok {
				return s, nil
			}
			if attempt < maxReserveAttempts {
				_ = c.Close()
				continue
			}
		}
		if s.lease, err = acquireEchoID(c); err != nil {
			return nil, err
		}
		return s, nil
	}
}

// acquireEchoID takes a free echo ID; without one it closes c.
func acquireEchoID(c packetConn) (*icmpid.Lease, error) {
	lease, err := icmpid.Acquire()
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("cannot allocate ICMP echo ID: %w", err)
	}
	return lease, nil
}

// kernelEchoID returns the echo ID Linux gave a datagram socket, which is
// its local port, or 0 when unknown.
func kernelEchoID(c packetConn) uint16 {
	if l, ok := c.(interface{ LocalAddr() net.Addr }); ok {
		if a, ok := l.LocalAddr().(*net.UDPAddr); ok {
			return uint16(a.Port)
		}
	}
	return 0
}

// isDenied reports whether err is a lack of privileges. Winsock's WSAEACCES
// is not the syscall.EACCES that fs.ErrPermission stands for.
func isDenied(err error) bool {
	const wsaEACCES = syscall.Errno(10013)
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, wsaEACCES)
}

// datagramUnavailable reports whether err means that this process may not
// use datagram ICMP (EACCES outside of ping_group_range, EPERM from a
// sandbox) or that the kernel does not have it.
func datagramUnavailable(err error) bool {
	return isDenied(err) || errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.ESOCKTNOSUPPORT)
}

// addressHint says how to fix a socket that cannot be bound, if that is
// what err is about.
func addressHint(err error) string {
	if errors.Is(err, syscall.EADDRNOTAVAIL) {
		return " (check source_interface: the address must be one of this host)"
	}
	return ""
}

// privilegeHint says how to get permission to open an ICMP socket.
func privilegeHint(goos string) string {
	switch goos {
	case "linux":
		return ` (allow datagram ICMP for your group with: sudo sysctl -w net.ipv4.ping_group_range="0 2147483647";` +
			` or allow mping to use raw sockets with: sudo setcap cap_net_raw=+ep /path/to/mping)`
	case "windows":
		return " (run as Administrator)"
	}
	return " (run as root)"
}

// datagramConn makes a datagram ICMP socket look like a raw one, which
// speaks *net.IPAddr: the Go runtime takes it for a UDP socket and wants
// and reports *net.UDPAddr, whose ports mean nothing here.
type datagramConn struct{ packetConn }

func (c datagramConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	if a, ok := dst.(*net.IPAddr); ok {
		dst = &net.UDPAddr{IP: a.IP, Zone: a.Zone}
	}
	return c.packetConn.WriteTo(b, dst)
}

func (c datagramConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, src, err := c.packetConn.ReadFrom(b)
	return n, peerIPAddr(src), err
}

// ReadFromTTL reports a TTL of 0 (unknown) if the connection cannot tell it.
func (c datagramConn) ReadFromTTL(b []byte) (int, int, net.Addr, error) {
	tc, ok := c.packetConn.(ttlPacketConn)
	if !ok {
		n, src, err := c.ReadFrom(b)
		return n, 0, src, err
	}
	n, ttl, src, err := tc.ReadFromTTL(b)
	return n, ttl, peerIPAddr(src), err
}

// peerIPAddr converts the sender of a datagram to an *net.IPAddr, whose
// String is the key of a target: no port, but the zone if there is one.
func peerIPAddr(a net.Addr) net.Addr {
	if u, ok := a.(*net.UDPAddr); ok && u != nil {
		return &net.IPAddr{IP: u.IP, Zone: u.Zone}
	}
	return a
}
