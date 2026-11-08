package trace

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	protocolICMP   = 1
	protocolUDP    = 17
	protocolICMPv6 = 58

	ipv4MinHeaderLen = 20
	ipv6HeaderLen    = 40
	icmpEchoHdrLen   = 8
	udpHeaderLen     = 8
)

// replyKind classifies a received ICMP message relevant to tracing
type replyKind int

const (
	kindEchoReply   replyKind = iota + 1 // destination answered
	kindTimeExceed                       // an intermediate router answered
	kindUnreachable                      // the destination (UDP: port unreachable) or a router reported it unreachable
)

// reply is a parsed ICMP message matched back to the probe we sent. For ICMP
// probes id/seq are the echo ID and sequence; for UDP probes they are the
// source and destination port of the quoted datagram.
type reply struct {
	kind replyKind
	id   uint16
	seq  uint16
	ttl  int        // TTL the probe was sent with, if the reply tells (0 = unknown)
	dst  netip.Addr // destination of the quoted datagram (invalid for echo replies)
}

// sameAddr reports whether the quoted destination a is ip; an unknown
// (invalid) a matches.
func sameAddr(ip net.IP, a netip.Addr) bool {
	if !a.IsValid() {
		return true
	}
	b, ok := netip.AddrFromSlice(ip)
	return ok && b.Unmap() == a.Unmap()
}

// udpReplyTTL returns the probe TTL encoded in a UDP probe's length by
// udpProbePayload (0 if it does not look like one of our probes)
func udpReplyTTL(udpLen int) int {
	ttl := udpLen - udpHeaderLen - len(udpPayload)
	if ttl < 1 || ttl > maxHops {
		return 0
	}
	return ttl
}

var errNotOurs = errors.New("not a reply to one of our probes")

// parseReply parses an ICMP message (without the outer IP header) received
// while tracing with ICMP echo. For error messages (Time Exceeded /
// Destination Unreachable) the echo ID and sequence are taken from the quoted
// original datagram.
func parseReply(v6 bool, b []byte) (reply, error) {
	m, err := parseICMP(v6, b)
	if err != nil {
		return reply{}, err
	}
	if echo, ok := m.Body.(*icmp.Echo); ok {
		if m.Type != ipv4.ICMPTypeEchoReply && m.Type != ipv6.ICMPTypeEchoReply {
			return reply{}, errNotOurs
		}
		return reply{kind: kindEchoReply, id: uint16(echo.ID), seq: uint16(echo.Seq)}, nil
	}
	kind, data, err := icmpError(m)
	if err != nil {
		return reply{}, err
	}
	proto := protocolICMP
	echoType := byte(ipv4.ICMPTypeEcho)
	if v6 {
		proto, echoType = protocolICMPv6, byte(ipv6.ICMPTypeEchoRequest)
	}
	inner, dst, err := quotedTransport(v6, data, proto, icmpEchoHdrLen)
	if err != nil {
		return reply{}, err
	}
	if inner[0] != echoType {
		return reply{}, errNotOurs
	}
	return reply{
		kind: kind,
		id:   binary.BigEndian.Uint16(inner[4:6]),
		seq:  binary.BigEndian.Uint16(inner[6:8]),
		dst:  dst,
	}, nil
}

// parseUDPReply parses an ICMP error received while tracing with UDP. The
// quoted UDP header identifies the probe: id is its source port, seq its
// destination port.
func parseUDPReply(v6 bool, b []byte) (reply, error) {
	m, err := parseICMP(v6, b)
	if err != nil {
		return reply{}, err
	}
	kind, data, err := icmpError(m)
	if err != nil {
		return reply{}, err
	}
	inner, dst, err := quotedTransport(v6, data, protocolUDP, udpHeaderLen)
	if err != nil {
		return reply{}, err
	}
	return reply{
		kind: kind,
		id:   binary.BigEndian.Uint16(inner[0:2]),
		seq:  binary.BigEndian.Uint16(inner[2:4]),
		ttl:  udpReplyTTL(int(binary.BigEndian.Uint16(inner[4:6]))),
		dst:  dst,
	}, nil
}

func parseICMP(v6 bool, b []byte) (*icmp.Message, error) {
	proto := protocolICMP
	if v6 {
		proto = protocolICMPv6
	}
	return icmp.ParseMessage(proto, b)
}

// icmpError returns the kind and the quoted original datagram of a Time
// Exceeded or Destination Unreachable message
func icmpError(m *icmp.Message) (replyKind, []byte, error) {
	switch m.Type {
	case ipv4.ICMPTypeTimeExceeded, ipv6.ICMPTypeTimeExceeded:
		if body, ok := m.Body.(*icmp.TimeExceeded); ok {
			return kindTimeExceed, body.Data, nil
		}
	case ipv4.ICMPTypeDestinationUnreachable, ipv6.ICMPTypeDestinationUnreachable:
		if body, ok := m.Body.(*icmp.DstUnreach); ok {
			return kindUnreachable, body.Data, nil
		}
	}
	return 0, nil, errNotOurs
}

// IPv6 extension headers that may precede the transport header of a quoted
// datagram. Our probes carry none, but a router may still quote some.
const (
	ipv6HopByHop    = 0
	ipv6Routing     = 43
	ipv6Fragment    = 44
	ipv6DestOptions = 60
)

// quotedTransport skips the IP header of the original datagram quoted in an
// ICMP error and returns its transport header, which must be of protocol
// proto and at least minLen bytes long, and its destination address.
func quotedTransport(v6 bool, data []byte, proto, minLen int) ([]byte, netip.Addr, error) {
	var next, off int
	var dst netip.Addr
	if v6 {
		if len(data) < ipv6HeaderLen {
			return nil, dst, errNotOurs
		}
		dst = netip.AddrFrom16([16]byte(data[24:40]))
		next, off = int(data[6]), ipv6HeaderLen
		for next != proto {
			if len(data) < off+8 {
				return nil, dst, errNotOurs
			}
			switch next {
			case ipv6HopByHop, ipv6Routing, ipv6DestOptions:
				next, off = int(data[off]), off+(int(data[off+1])+1)*8
			case ipv6Fragment:
				next, off = int(data[off]), off+8
			default:
				return nil, dst, errNotOurs
			}
		}
	} else {
		if len(data) < ipv4MinHeaderLen {
			return nil, dst, errNotOurs
		}
		dst = netip.AddrFrom4([4]byte(data[16:20]))
		next, off = int(data[9]), int(data[0]&0x0f)*4
		if off < ipv4MinHeaderLen {
			return nil, dst, errNotOurs
		}
	}
	if next != proto || len(data) < off+minLen {
		return nil, dst, errNotOurs
	}
	return data[off:], dst, nil
}
