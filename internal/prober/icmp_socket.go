package prober

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/net/icmp"

	"github.com/servak/mping/internal/icmpid"
	"github.com/servak/mping/internal/icmpsock"
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
	if icmpsock.DatagramSupported(goos) {
		var s *icmpSocket
		if s, dgramErr = openDatagram(goos, dgramNet, address, listen); dgramErr == nil {
			return s, nil
		}
		if !icmpsock.DatagramUnavailable(dgramErr) {
			return nil, fmt.Errorf("cannot open a datagram ICMP socket on %s: %w%s", address, dgramErr, addressHint(dgramErr))
		}
	}

	c, err := listen(rawNet, address)
	if err == nil {
		lease, err := icmpsock.AcquireID(c, icmpid.Default())
		if err != nil {
			return nil, err
		}
		return &icmpSocket{conn: c, typ: rawSocket, checkID: true, lease: lease}, nil
	}

	hint := addressHint(err)
	if dgramErr == nil { // raw sockets are all there is on goos
		if icmpsock.Denied(err) {
			hint = icmpsock.PrivilegeHint(goos)
		}
		return nil, fmt.Errorf("cannot open a raw ICMP socket on %s: %w%s", address, err, hint)
	}
	if hint == "" {
		hint = icmpsock.PrivilegeHint(goos)
	}
	return nil, fmt.Errorf("cannot open an ICMP socket on %s: datagram: %w; raw: %w%s", address, dgramErr, err, hint)
}

// openDatagram opens a datagram ICMP socket and takes its echo ID, see
// icmpsock.OpenDatagram.
func openDatagram(goos, network, address string, listen listenFunc) (*icmpSocket, error) {
	c, lease, err := icmpsock.OpenDatagram(goos, icmpid.Default(), func() (packetConn, error) {
		return listen(network, address)
	})
	if err != nil {
		return nil, err
	}
	return &icmpSocket{conn: datagramConn{c}, typ: datagramSocket, checkID: icmpsock.DatagramKeepsEchoID(goos), lease: lease}, nil
}

// addressHint says how to fix a socket that cannot be bound, if that is
// what err is about.
func addressHint(err error) string {
	if errors.Is(err, syscall.EADDRNOTAVAIL) {
		return " (check source_interface: the address must be one of this host)"
	}
	return ""
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
