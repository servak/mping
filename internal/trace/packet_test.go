package trace

import (
	"net"
	"net/netip"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
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

func TestEchoIDAvoidsProberID(t *testing.T) {
	pid := 0x12345
	if id := echoID(0x2344, 1, pid); id == uint16(pid&0xffff) {
		t.Errorf("echo ID %#x equals the ICMP prober ID", id)
	}
	// IDs follow the random base, not the pid: a tracer of one process
	// must not take the prober ID of the next one (pid+1)
	if a, b := echoID(0x1000, 1, pid), echoID(0x1000, 1, pid+1); a != b {
		t.Errorf("echo ID depends on the pid: %#x / %#x", a, b)
	}
}

// Replies with our echo ID but about another destination (another mping
// process using the same ID) are ignored
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
		tp := &icmpTransport{v6: v6, dst: dst, id: 42, checkID: true}
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
			peer net.Addr
			want bool
		}{
			{"echo reply from the destination", echo, &net.IPAddr{IP: dst}, true},
			{"echo reply from another host", echo, &net.IPAddr{IP: other}, false},
			{"echo reply with another ID", reply{kind: kindEchoReply, id: 43, seq: 3}, &net.IPAddr{IP: dst}, false},
			{"time exceeded for our probe", te(dst, 42), &net.IPAddr{IP: other}, true},
			{"time exceeded for another destination", te(other, 42), &net.IPAddr{IP: other}, false},
			{"time exceeded with another ID", te(dst, 43), &net.IPAddr{IP: other}, false},
		}
		for _, tt := range tests {
			if got := tp.accept(tt.r, tt.peer); got != tt.want {
				t.Errorf("v6=%v %s: accept = %v, want %v", v6, tt.name, got, tt.want)
			}
		}
	}
}
