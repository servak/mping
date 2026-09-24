package trace

import (
	"encoding/binary"
	"errors"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	protocolICMP   = 1
	protocolICMPv6 = 58

	ipv4MinHeaderLen = 20
	ipv6HeaderLen    = 40
	icmpEchoHdrLen   = 8
)

// replyKind classifies a received ICMP message relevant to tracing
type replyKind int

const (
	kindEchoReply   replyKind = iota + 1 // destination answered
	kindTimeExceed                       // an intermediate router answered
	kindUnreachable                      // a router reported the destination unreachable
)

// reply is a parsed ICMP message matched back to the echo request we sent.
type reply struct {
	kind replyKind
	id   uint16
	seq  uint16
}

var errNotOurs = errors.New("not a reply to an echo request")

// parseReply parses an ICMP message (without the outer IP header). For
// error messages (Time Exceeded / Destination Unreachable) the echo ID and
// sequence are taken from the quoted original datagram.
func parseReply(v6 bool, b []byte) (reply, error) {
	proto := protocolICMP
	if v6 {
		proto = protocolICMPv6
	}
	m, err := icmp.ParseMessage(proto, b)
	if err != nil {
		return reply{}, err
	}

	switch m.Type {
	case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
		echo, ok := m.Body.(*icmp.Echo)
		if !ok {
			return reply{}, errNotOurs
		}
		return reply{kind: kindEchoReply, id: uint16(echo.ID), seq: uint16(echo.Seq)}, nil
	case ipv4.ICMPTypeTimeExceeded, ipv6.ICMPTypeTimeExceeded:
		body, ok := m.Body.(*icmp.TimeExceeded)
		if !ok {
			return reply{}, errNotOurs
		}
		return parseQuoted(v6, body.Data, kindTimeExceed)
	case ipv4.ICMPTypeDestinationUnreachable, ipv6.ICMPTypeDestinationUnreachable:
		body, ok := m.Body.(*icmp.DstUnreach)
		if !ok {
			return reply{}, errNotOurs
		}
		return parseQuoted(v6, body.Data, kindUnreachable)
	}
	return reply{}, errNotOurs
}

// parseQuoted extracts the echo ID/seq from the original datagram quoted in
// an ICMP error: the original IP header followed by at least 8 bytes of the
// original ICMP echo request.
func parseQuoted(v6 bool, data []byte, kind replyKind) (reply, error) {
	var hdrLen int
	if v6 {
		// Extension headers are not used for our probes
		if len(data) < ipv6HeaderLen || data[6] != protocolICMPv6 {
			return reply{}, errNotOurs
		}
		hdrLen = ipv6HeaderLen
	} else {
		if len(data) < ipv4MinHeaderLen || data[9] != protocolICMP {
			return reply{}, errNotOurs
		}
		hdrLen = int(data[0]&0x0f) * 4
		if hdrLen < ipv4MinHeaderLen {
			return reply{}, errNotOurs
		}
	}
	if len(data) < hdrLen+icmpEchoHdrLen {
		return reply{}, errNotOurs
	}
	inner := data[hdrLen:]
	echoType := byte(ipv4.ICMPTypeEcho)
	if v6 {
		echoType = byte(ipv6.ICMPTypeEchoRequest)
	}
	if inner[0] != echoType {
		return reply{}, errNotOurs
	}
	return reply{
		kind: kind,
		id:   binary.BigEndian.Uint16(inner[4:6]),
		seq:  binary.BigEndian.Uint16(inner[6:8]),
	}, nil
}
