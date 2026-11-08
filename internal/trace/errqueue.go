package trace

import (
	"bytes"
	"encoding/binary"
	"net"
)

// Linux error queue (IP_RECVERR / IPV6_RECVERR) message layout. The parser
// is portable so that it can be tested everywhere; only reading the queue is
// Linux specific (transport_udp_linux.go).
const (
	soEEOriginICMP  = 2 // SO_EE_ORIGIN_ICMP
	soEEOriginICMP6 = 3 // SO_EE_ORIGIN_ICMP6

	sockExtendedErrLen = 16 // sizeof(struct sock_extended_err)

	linuxAFInet  = 2
	linuxAFInet6 = 10

	icmpv4TimeExceeded = 11
	icmpv4DstUnreach   = 3
	icmpv6DstUnreach   = 1
	icmpv6TimeExceeded = 3
)

// parseExtendedErr parses the data of an IP_RECVERR / IPV6_RECVERR control
// message: a struct sock_extended_err followed by the offender address
// (SO_EE_OFFENDER), the router or host that sent the ICMP error.
//
//	struct sock_extended_err {
//		__u32 ee_errno; __u8 ee_origin; __u8 ee_type; __u8 ee_code;
//		__u8 ee_pad; __u32 ee_info; __u32 ee_data;
//	};
//
// dstPort is the destination port of the datagram that caused the error,
// taken from msg_name. The returned reply has seq set to it (id is left 0:
// the error queue belongs to our socket, so the source port is ours).
func parseExtendedErr(data []byte, dstPort uint16) (reply, string, error) {
	if len(data) < sockExtendedErrLen {
		return reply{}, "", errNotOurs
	}
	origin, typ := data[4], data[5]
	var kind replyKind
	switch {
	case origin == soEEOriginICMP && typ == icmpv4TimeExceeded,
		origin == soEEOriginICMP6 && typ == icmpv6TimeExceeded:
		kind = kindTimeExceed
	case origin == soEEOriginICMP && typ == icmpv4DstUnreach,
		origin == soEEOriginICMP6 && typ == icmpv6DstUnreach:
		kind = kindUnreachable
	default:
		return reply{}, "", errNotOurs // e.g. a local error (SO_EE_ORIGIN_LOCAL)
	}
	from := parseSockaddr(data[sockExtendedErrLen:])
	if from == nil {
		return reply{}, "", errNotOurs
	}
	return reply{kind: kind, seq: dstPort}, from.String(), nil
}

// parseSockaddr parses a Linux struct sockaddr_in / sockaddr_in6. The family
// is in host byte order, the address in network byte order.
func parseSockaddr(b []byte) net.IP {
	if len(b) < 2 {
		return nil
	}
	switch binary.NativeEndian.Uint16(b[0:2]) {
	case linuxAFInet: // family, port, addr[4]
		if len(b) >= 8 {
			return net.IP(append([]byte(nil), b[4:8]...))
		}
	case linuxAFInet6: // family, port, flowinfo, addr[16], scope_id
		if len(b) >= 24 {
			return net.IP(append([]byte(nil), b[8:24]...))
		}
	}
	return nil
}

// errQueuePayloadTTL returns the probe TTL from the payload of the original
// datagram that the error queue returns with an ICMP error (see
// udpProbePayload), or 0 if the payload is truncated or not ours.
func errQueuePayloadTTL(payload []byte) int {
	if len(payload) <= len(udpPayload) || !bytes.HasPrefix(payload, udpPayload) {
		return 0
	}
	ttl := int(payload[len(udpPayload)])
	if ttl < 1 || ttl > maxHops {
		return 0
	}
	return ttl
}
