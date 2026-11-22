package trace

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// Linux error queue (IP_RECVERR / IPV6_RECVERR) message layout. The parser
// is portable so that it can be tested everywhere; only reading the queue is
// Linux specific (errqueue_linux.go).
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

// queuedError is an ICMP error read from the error queue of a socket.
type queuedError struct {
	// payload is what the ICMP error quotes of the datagram we sent: for a
	// UDP socket from the UDP payload on, for a datagram ICMP socket from
	// the ICMP header on (ping_err in the kernel, IPv4 and IPv6 alike).
	payload []byte
	dst     netip.AddrPort // destination of that datagram
	data    []byte         // of the IP_RECVERR / IPV6_RECVERR control message
	at      time.Time      // when it was read
}

// icmpErrQueueReply returns the reply that the ICMP error e of a datagram
// ICMP socket stands for and its sender. Its id is the one the kernel gave
// the socket, which it has matched already. (The kernel drops ICMP errors
// that do not quote the whole 8 byte header, like quotedTransport.)
func icmpErrQueueReply(v6 bool, e queuedError) (reply, string, error) {
	id, seq, err := quotedEcho(v6, e.payload)
	if err != nil {
		return reply{}, "", err
	}
	r, from, err := parseExtendedErr(e.data, seq)
	if err != nil {
		return reply{}, "", err
	}
	r.id, r.dst = id, e.dst.Addr()
	return r, from, nil
}

// msgQueue names the queue of a socket that a message came from.
type msgQueue int

const (
	noQueue   msgQueue = iota
	errQueue           // the error queue
	dataQueue          // the normal receive queue
)

// maxReadFailures is how many reads may fail in a row before queuePoller
// waits for the socket instead of trying again.
const maxReadFailures = 16

// queuePoller reads the error queue and the receive queue of a socket with
// IP_RECVERR, which are both announced by the same wakeup. The zero value is
// ready to use.
type queuePoller struct{ failures int }

// next reads one message with readErr from the error queue or, when that is
// empty, with readData, and tells where it came from. Both must not block.
// With wait set, both queues are empty (EAGAIN) and the caller has to wait for
// the socket to become readable; otherwise there may be more to read. The
// wakeup is edge triggered, so nothing must be left unread before waiting.
//
// Linux reports every ICMP error once more as the result of a socket call
// (SO_ERROR), which only clears it: such a failure, or an EINTR, says nothing
// about the queues, so next has to be called again.
func (p *queuePoller) next(readErr, readData func() error) (from msgQueue, wait bool) {
	for _, q := range []struct {
		from msgQueue
		read func() error
	}{{errQueue, readErr}, {dataQueue, readData}} {
		err := q.read()
		switch {
		case err == nil:
			p.failures = 0
			return q.from, false
		case errors.Is(err, syscall.EAGAIN): // empty
		case p.failures < maxReadFailures:
			p.failures++
			return noQueue, false
		}
	}
	p.failures = 0
	return noQueue, true
}

// sendRetries is how often a send is retried when the socket reports an
// earlier ICMP error instead of sending
const sendRetries = 3

// retrySend sends a probe on a socket with IP_RECVERR. Linux also reports a
// received ICMP error once as the result of the next socket call, without
// sending anything. The error itself is still on the error queue for receive,
// so just send again.
func retrySend(send func() error) error {
	for i := 0; ; i++ {
		err := send()
		if err == nil || i >= sendRetries || !isICMPErrno(err) {
			return err
		}
	}
}

// parseExtendedErr parses the data of an IP_RECVERR / IPV6_RECVERR control
// message: a struct sock_extended_err followed by the offender address
// (SO_EE_OFFENDER), the router or host that sent the ICMP error.
//
//	struct sock_extended_err {
//		__u32 ee_errno; __u8 ee_origin; __u8 ee_type; __u8 ee_code;
//		__u8 ee_pad; __u32 ee_info; __u32 ee_data;
//	};
//
// seq is the key of the probe that caused the error; the returned reply has
// seq set to it.
func parseExtendedErr(data []byte, seq uint16) (reply, string, error) {
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
	return reply{kind: kind, seq: seq}, from.String(), nil
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
