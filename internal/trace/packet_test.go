package trace

import (
	"net"
	"net/netip"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/servak/mping/internal/icmpid"
)

func echoRequest(t *testing.T, v6 bool, id, seq int) []byte {
	t.Helper()
	m := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("x")}}
	if v6 {
		m.Type = ipv6.ICMPTypeEchoRequest
	}
	b, err := m.Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// quoted builds the original datagram quoted inside an ICMP error
func quoted(t *testing.T, v6 bool, ihl int, id, seq int) []byte {
	t.Helper()
	echo := echoRequest(t, v6, id, seq)
	if v6 {
		hdr := make([]byte, ipv6HeaderLen)
		hdr[0] = 0x60
		hdr[6] = protocolICMPv6
		return append(hdr, echo[:8]...)
	}
	hdr := make([]byte, ihl)
	hdr[0] = 0x40 | byte(ihl/4)
	hdr[9] = protocolICMP
	return append(hdr, echo[:8]...)
}

func marshal(t *testing.T, m icmp.Message) []byte {
	t.Helper()
	b, err := m.Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseReply(t *testing.T) {
	tests := []struct {
		name string
		v6   bool
		msg  []byte
		want reply
	}{
		{
			name: "v4 echo reply",
			msg:  marshal(t, icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 0x1234, Seq: 7}}),
			want: reply{kind: kindEchoReply, id: 0x1234, seq: 7},
		},
		{
			name: "v4 time exceeded",
			msg:  marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted(t, false, 20, 0x1234, 3)}}),
			want: reply{kind: kindTimeExceed, id: 0x1234, seq: 3},
		},
		{
			name: "v4 time exceeded with IP options (IHL=6)",
			msg:  marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted(t, false, 24, 0xbeef, 65535)}}),
			want: reply{kind: kindTimeExceed, id: 0xbeef, seq: 65535},
		},
		{
			name: "v4 destination unreachable",
			msg:  marshal(t, icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Code: 1, Body: &icmp.DstUnreach{Data: quoted(t, false, 20, 1, 9)}}),
			want: reply{kind: kindUnreachable, id: 1, seq: 9},
		},
		{
			name: "v6 echo reply",
			v6:   true,
			msg:  marshal(t, icmp.Message{Type: ipv6.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 42, Seq: 5}}),
			want: reply{kind: kindEchoReply, id: 42, seq: 5},
		},
		{
			name: "v6 time exceeded",
			v6:   true,
			msg:  marshal(t, icmp.Message{Type: ipv6.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted(t, true, 0, 42, 11)}}),
			want: reply{kind: kindTimeExceed, id: 42, seq: 11},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReply(tt.v6, tt.msg)
			if err != nil {
				t.Fatalf("parseReply() error = %v", err)
			}
			got.dst = netip.Addr{} // checked in TestICMPAccept
			if got != tt.want {
				t.Errorf("parseReply() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseReplyRejects(t *testing.T) {
	// Quoted datagram is a UDP packet (e.g. someone else's traceroute)
	udp := quoted(t, false, 20, 1, 1)
	udp[9] = 17
	// Quoted echo *reply* instead of request
	notRequest := quoted(t, false, 20, 1, 1)
	notRequest[20] = byte(ipv4.ICMPTypeEchoReply)

	tests := map[string][]byte{
		"echo request":    echoRequest(t, false, 1, 1),
		"quoted udp":      marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: udp}}),
		"quoted not echo": marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: notRequest}}),
		"truncated quote": marshal(t, icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted(t, false, 20, 1, 1)[:24]}}),
		"garbage":         {0xff},
	}
	for name, msg := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := parseReply(false, msg); err == nil {
				t.Errorf("expected error, got %+v", got)
			}
		})
	}
}

// A tracer's echo ID comes from the process-wide allocator shared with the
// ICMP probers: it is held while the transport is open, other allocations
// (probers, other tracers) never get it, and closing releases it once.
func TestICMPTransportEchoIDShared(t *testing.T) {
	tp, err := newICMPTransport(&net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}, false)
	if err != nil {
		t.Skipf("no ICMP socket: %v", err)
	}
	if tp.id == 0 || !icmpid.InUse(tp.id) {
		t.Fatalf("echo ID %#x not held in the shared allocator", tp.id)
	}
	for range 1000 {
		id := icmpid.Acquire()
		if id == tp.id {
			t.Fatalf("shared allocator handed out the tracer ID %#x", id)
		}
		defer icmpid.Release(id)
	}
	_ = tp.close()
	if icmpid.InUse(tp.id) {
		t.Fatalf("echo ID %#x still held after close", tp.id)
	}
	_ = tp.close() // closing twice must not release an ID reacquired since
}

// The echo ID decides whether a reply is ours. Replies from, or quoting, an
// address other than the destination are accepted: DNAT and load balancers
// answer from another address or leave the quoted destination unrewritten.
func TestICMPAccept(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		dst, other := net.ParseIP("192.0.2.1"), net.ParseIP("198.51.100.1")
		dstOff, dstLen := 16, 4
		if v6 {
			dst, other = net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")
			dstOff, dstLen = 24, 16
		}
		raw := func(ip net.IP) []byte {
			if v6 {
				return ip.To16()
			}
			return ip.To4()
		}
		tp := &icmpTransport{v6: v6, id: 42, checkID: true}
		teType := icmp.Type(ipv4.ICMPTypeTimeExceeded)
		if v6 {
			teType = ipv6.ICMPTypeTimeExceeded
		}
		te := func(to net.IP, id int) reply {
			q := quoted(t, v6, 20, id, 3)
			copy(q[dstOff:dstOff+dstLen], raw(to))
			r, err := parseReply(v6, marshal(t, icmp.Message{Type: teType, Body: &icmp.TimeExceeded{Data: q}}))
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		echo := reply{kind: kindEchoReply, id: 42, seq: 3}
		tests := []struct {
			name string
			r    reply
			want bool
		}{
			{"echo reply from the destination", echo, true},
			{"echo reply from another host (DNAT)", echo, true},
			{"echo reply with another ID", reply{kind: kindEchoReply, id: 43, seq: 3}, false},
			{"time exceeded for our probe", te(dst, 42), true},
			{"time exceeded quoting another destination (DNAT)", te(other, 42), true},
			{"time exceeded with another ID", te(dst, 43), false},
		}
		for _, tt := range tests {
			if got := tp.accept(tt.r); got != tt.want {
				t.Errorf("v6=%v %s: accept = %v, want %v", v6, tt.name, got, tt.want)
			}
		}
		// Linux datagram sockets: the kernel already matched the reply
		// by its own echo ID
		dgram := &icmpTransport{v6: v6, id: 42}
		if !dgram.accept(te(other, 7)) || !dgram.accept(reply{kind: kindEchoReply, id: 7, seq: 3}) {
			t.Errorf("v6=%v: datagram socket rejected a reply", v6)
		}
	}
}
