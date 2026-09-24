package trace

import (
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
