package trace

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// quotedUDP builds the original UDP datagram quoted inside an ICMP error:
// the IP header (ihl bytes for IPv4; for IPv6 the fixed header plus ext, a
// list of extension header protocol numbers) and the 8 byte UDP header.
func quotedUDP(v6 bool, ihl int, ext []byte, src, dst uint16) []byte {
	udp := make([]byte, udpHeaderLen)
	binary.BigEndian.PutUint16(udp[0:2], src)
	binary.BigEndian.PutUint16(udp[2:4], dst)
	binary.BigEndian.PutUint16(udp[4:6], udpHeaderLen+uint16(len(udpPayload)))
	if !v6 {
		hdr := make([]byte, ihl)
		hdr[0] = 0x40 | byte(ihl/4)
		hdr[9] = protocolUDP
		return append(hdr, udp...)
	}
	hdr := make([]byte, ipv6HeaderLen)
	hdr[0] = 0x60
	next := &hdr[6]
	for _, proto := range ext {
		*next = proto
		eh := make([]byte, 8) // next header, length 0 (= 8 bytes)
		hdr = append(hdr, eh...)
		next = &hdr[len(hdr)-8]
	}
	*next = protocolUDP
	return append(hdr, udp...)
}

func TestParseUDPReply(t *testing.T) {
	te4 := func(q []byte) []byte {
		return marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: q}})
	}
	pu4 := func(q []byte) []byte {
		return marshal(t, icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Code: 3, Body: &icmp.DstUnreach{Data: q}})
	}
	te6 := func(q []byte) []byte {
		return marshal(t, icmp.Message{Type: ipv6.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: q}})
	}
	pu6 := func(q []byte) []byte {
		return marshal(t, icmp.Message{Type: ipv6.ICMPTypeDestinationUnreachable, Code: 4, Body: &icmp.DstUnreach{Data: q}})
	}
	tests := []struct {
		name string
		v6   bool
		msg  []byte
		want reply
	}{
		{"v4 time exceeded", false, te4(quotedUDP(false, 20, nil, 50000, 33434)), reply{kindTimeExceed, 50000, 33434}},
		{"v4 time exceeded with IP options", false, te4(quotedUDP(false, 28, nil, 1, 33497)), reply{kindTimeExceed, 1, 33497}},
		{"v4 port unreachable", false, pu4(quotedUDP(false, 20, nil, 50000, 33440)), reply{kindUnreachable, 50000, 33440}},
		{"v6 time exceeded", true, te6(quotedUDP(true, 0, nil, 40000, 33435)), reply{kindTimeExceed, 40000, 33435}},
		{"v6 port unreachable", true, pu6(quotedUDP(true, 0, nil, 40000, 33436)), reply{kindUnreachable, 40000, 33436}},
		{"v6 with extension headers", true, te6(quotedUDP(true, 0, []byte{ipv6HopByHop, ipv6DestOptions, ipv6Fragment}, 7, 33437)),
			reply{kindTimeExceed, 7, 33437}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUDPReply(tt.v6, tt.msg)
			if err != nil {
				t.Fatalf("parseUDPReply() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("parseUDPReply() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseUDPReplyRejects(t *testing.T) {
	icmpQuote := quoted(t, false, 20, 1, 1) // an ICMP echo, not UDP
	tests := map[string]struct {
		v6  bool
		msg []byte
	}{
		"echo reply": {false, marshal(t, icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 1, Seq: 1}})},
		"quoted icmp": {false, marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded,
			Body: &icmp.TimeExceeded{Data: icmpQuote}})},
		"truncated udp header": {false, marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded,
			Body: &icmp.TimeExceeded{Data: quotedUDP(false, 20, nil, 1, 2)[:24]}})},
		"v6 unknown extension": {true, marshal(t, icmp.Message{Type: ipv6.ICMPTypeTimeExceeded,
			Body: &icmp.TimeExceeded{Data: quotedUDP(true, 0, []byte{50}, 1, 2)}})}, // ESP
		"garbage": {false, []byte{0xff}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := parseUDPReply(tt.v6, tt.msg); err == nil {
				t.Errorf("expected error, got %+v", got)
			}
		})
	}
	// ICMP tracing must in turn ignore errors about UDP datagrams
	msg := marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quotedUDP(false, 20, nil, 1, 2)}})
	if got, err := parseReply(false, msg); err == nil {
		t.Errorf("parseReply() of a UDP quote = %+v, want error", got)
	}
}

// extendedErr builds an IP_RECVERR control message payload
func extendedErr(origin, typ, code byte, offender net.IP) []byte {
	b := make([]byte, sockExtendedErrLen)
	b[4], b[5], b[6] = origin, typ, code
	if ip4 := offender.To4(); ip4 != nil {
		sa := make([]byte, 16)
		binary.NativeEndian.PutUint16(sa[0:2], linuxAFInet)
		copy(sa[4:8], ip4)
		return append(b, sa...)
	}
	sa := make([]byte, 28)
	binary.NativeEndian.PutUint16(sa[0:2], linuxAFInet6)
	copy(sa[8:24], offender.To16())
	return append(b, sa...)
}

func TestParseExtendedErr(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantKind replyKind
		wantFrom string
	}{
		{"v4 time exceeded", extendedErr(soEEOriginICMP, 11, 0, net.ParseIP("192.0.2.1")), kindTimeExceed, "192.0.2.1"},
		{"v4 port unreachable", extendedErr(soEEOriginICMP, 3, 3, net.ParseIP("198.51.100.7")), kindUnreachable, "198.51.100.7"},
		{"v6 time exceeded", extendedErr(soEEOriginICMP6, 3, 0, net.ParseIP("2001:db8::1")), kindTimeExceed, "2001:db8::1"},
		{"v6 port unreachable", extendedErr(soEEOriginICMP6, 1, 4, net.ParseIP("2001:db8::2")), kindUnreachable, "2001:db8::2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, from, err := parseExtendedErr(tt.data, 33440)
			if err != nil {
				t.Fatalf("parseExtendedErr() error = %v", err)
			}
			if r.kind != tt.wantKind || r.seq != 33440 || from != tt.wantFrom {
				t.Errorf("parseExtendedErr() = %+v from %s, want kind %d seq 33440 from %s", r, from, tt.wantKind, tt.wantFrom)
			}
		})
	}

	rejects := map[string][]byte{
		"local error":        extendedErr(1, 0, 0, net.ParseIP("192.0.2.1")), // SO_EE_ORIGIN_LOCAL
		"icmp echo reply":    extendedErr(soEEOriginICMP, 0, 0, net.ParseIP("192.0.2.1")),
		"short":              make([]byte, 8),
		"no offender":        extendedErr(soEEOriginICMP, 11, 0, net.ParseIP("192.0.2.1"))[:sockExtendedErrLen],
		"v6 packet too big":  extendedErr(soEEOriginICMP6, 2, 0, net.ParseIP("2001:db8::1")),
		"v4 source quench 4": extendedErr(soEEOriginICMP, 4, 0, net.ParseIP("192.0.2.1")),
	}
	for name, data := range rejects {
		t.Run("reject "+name, func(t *testing.T) {
			if r, from, err := parseExtendedErr(data, 1); err == nil {
				t.Errorf("expected error, got %+v from %s", r, from)
			}
		})
	}
}

func TestUDPPortRotation(t *testing.T) {
	for round := 1; round <= 3*UDPPortRange; round++ {
		seen := map[uint16]bool{}
		for ttl := 1; ttl <= maxHops; ttl++ {
			p := udpPort(round, ttl)
			if p < UDPBasePort || p >= UDPBasePort+UDPPortRange {
				t.Fatalf("udpPort(%d, %d) = %d out of range", round, ttl, p)
			}
			if seen[p] {
				t.Fatalf("round %d reuses port %d", round, p)
			}
			seen[p] = true
		}
		// The previous round may still have probes in flight
		for ttl := 1; ttl <= maxHops; ttl++ {
			if p := udpPort(round-1, ttl); seen[p] {
				t.Fatalf("rounds %d and %d share port %d", round-1, round, p)
			}
		}
	}
	// Every hop cycles through all flows and then repeats them
	for _, ttl := range []int{1, 2, 7, maxHops} {
		ports := map[uint16]bool{}
		for round := 1; round <= UDPPortRange; round++ {
			ports[udpPort(round, ttl)] = true
		}
		if len(ports) != UDPPortRange {
			t.Errorf("ttl %d uses %d ports in %d rounds, want all %d", ttl, len(ports), UDPPortRange, UDPPortRange)
		}
		if udpPort(1, ttl) != udpPort(1+UDPPortRange, ttl) {
			t.Errorf("ttl %d: ports do not repeat after %d rounds", ttl, UDPPortRange)
		}
	}
}

// fakeNetwork answers probes like a small network with ECMP: TTL 1 is a
// single router, TTL 2 hashes the destination port onto `branches` routers
// (the way routers hash the L4 ports), TTL 3 is the destination.
type fakeNetwork struct {
	branches int
	mu       sync.Mutex
	deliver  func(reply, string, time.Time)
	ready    chan struct{}
	closed   chan struct{}
	sent     []uint16
	sendErr  func(ttl int) error // optional: fail sends
}

func newFakeNetwork(branches int) *fakeNetwork {
	return &fakeNetwork{branches: branches, ready: make(chan struct{}), closed: make(chan struct{})}
}

func (f *fakeNetwork) privileged() bool { return false }

func (f *fakeNetwork) close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func (f *fakeNetwork) receive(deliver func(reply, string, time.Time)) {
	f.mu.Lock()
	f.deliver = deliver
	f.mu.Unlock()
	close(f.ready)
	<-f.closed
}

func (f *fakeNetwork) send(ttl int, port uint16) error {
	<-f.ready
	if f.sendErr != nil {
		if err := f.sendErr(ttl); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, port)
	r := reply{kind: kindTimeExceed, id: 1, seq: port}
	var from string
	switch ttl {
	case 1:
		from = "10.0.0.1"
	case 2:
		from = fmt.Sprintf("10.0.1.%d", int(port)%f.branches)
	default:
		from, r.kind = "192.0.2.10", kindUnreachable // port unreachable
	}
	f.deliver(r, from, time.Now().Add(time.Millisecond))
	return nil
}

func TestUDPTraceECMP(t *testing.T) {
	fake := newFakeNetwork(4)
	tr := newTracer(Config{Interval: 5 * time.Millisecond, Timeout: time.Second, NoDNS: true, Proto: ProtoUDP},
		[]byte{192, 0, 2, 10}, fake)
	if err := tr.Run(context.Background(), 2*UDPPortRange); err != nil {
		t.Fatal(err)
	}
	res := tr.Snapshot()
	if res.Proto != ProtoUDP || !res.Reached || len(res.Hops) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if got := len(res.Hops[0].Responders); got != 1 {
		t.Errorf("hop 1 has %d responders, want 1", got)
	}
	ecmp := res.Hops[1]
	if len(ecmp.Responders) != 4 || ecmp.Others != 3 {
		t.Fatalf("ECMP hop responders = %+v", ecmp.Responders)
	}
	for _, r := range ecmp.Responders {
		if r.Recv == 0 || r.Stale {
			t.Errorf("responder %+v", r)
		}
	}
	if ecmp.Loss() != 0 {
		t.Errorf("ECMP hop loss = %v", ecmp.Loss())
	}
	if dst := res.Hops[2]; dst.Addr != "192.0.2.10" || dst.Recv != dst.Sent {
		t.Errorf("destination hop = %+v", dst)
	}
	// Probes use the traceroute port range only
	for _, p := range fake.sent {
		if p < UDPBasePort || p >= UDPBasePort+UDPPortRange {
			t.Fatalf("probe to port %d", p)
		}
	}
}

func TestUDPLateReplyAndPortReuse(t *testing.T) {
	tr := newTracer(Config{NoDNS: true, Proto: ProtoUDP, Timeout: time.Second}, []byte{192, 0, 2, 10}, nil)
	var events []probeEvent
	tr.onEvent = func(e probeEvent) { events = append(events, e) }

	tr.mu.Lock()
	port := tr.probeKeyLocked(1, 2)
	tr.pending[port] = inflight{ttl: 2, round: 1, sent: testEpoch}
	tr.expire(testEpoch.Add(2 * time.Second)) // timed out
	tr.mu.Unlock()
	// The reply arrives after the probe was counted as lost
	tr.handle(reply{kind: kindTimeExceed, seq: port}, "10.0.0.2", testEpoch.Add(3*time.Second))
	if len(events) != 2 || events[0].outcome != outcomeLost || events[1].outcome != outcomeLate {
		t.Fatalf("events = %+v", events)
	}

	// A port still pending when it comes round again is counted as lost
	tr.mu.Lock()
	tr.pending[port] = inflight{ttl: 2, round: 1, sent: testEpoch}
	again := tr.probeKeyLocked(1+UDPPortRange, 2)
	tr.mu.Unlock()
	if again != port {
		t.Fatalf("port %d, want reuse of %d", again, port)
	}
	if _, ok := tr.pending[port]; ok || events[len(events)-1].outcome != outcomeLost {
		t.Errorf("stale pending probe not counted as lost: %+v", events)
	}
	if h := tr.hops[1]; h.sent != 2 || h.recv != 0 {
		t.Errorf("hop 2 sent=%d recv=%d, want 2/0", h.sent, h.recv)
	}
}

func TestUDPPortUnreachableEndsPath(t *testing.T) {
	tr := newTracer(Config{NoDNS: true, Proto: ProtoUDP}, []byte{192, 0, 2, 10}, nil)
	tr.mu.Lock()
	tr.rounds = 1
	for ttl := 1; ttl <= 5; ttl++ {
		tr.pending[udpPort(1, ttl)] = inflight{ttl: ttl, round: 1, sent: testEpoch}
	}
	tr.mu.Unlock()
	tr.handle(reply{kind: kindTimeExceed, seq: udpPort(1, 1)}, "10.0.0.1", testEpoch.Add(ms(1)))
	tr.handle(reply{kind: kindUnreachable, seq: udpPort(1, 3)}, "192.0.2.10", testEpoch.Add(ms(3)))
	tr.handle(reply{kind: kindUnreachable, seq: udpPort(1, 4)}, "192.0.2.10", testEpoch.Add(ms(3)))
	tr.handle(reply{kind: kindUnreachable, seq: udpPort(1, 2)}, "192.0.2.10", testEpoch.Add(ms(3)))
	res := tr.Snapshot()
	if !res.Reached || len(res.Hops) != 2 || res.Hops[1].Addr != "192.0.2.10" {
		t.Errorf("result = %+v", res)
	}
}
