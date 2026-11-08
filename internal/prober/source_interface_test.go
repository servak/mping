package prober

import (
	"net"
	"strings"
	"testing"
	"time"
)

func ipNet(s string) *net.IPNet {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

// An interface address is picked by preference: global unicast first, then
// link-local (with the interface as zone, needed to bind it), then anything
// else allowed (loopback when not skipped).
func TestPickInterfaceIP(t *testing.T) {
	addrs := func(cidrs ...string) []net.Addr {
		var a []net.Addr
		for _, c := range cidrs {
			a = append(a, ipNet(c))
		}
		return a
	}
	cases := []struct {
		name         string
		addrs        []net.Addr
		v6, skipLoop bool
		want         string
	}{
		{"v6 global after link-local", addrs("fe80::1/64", "2001:db8::1/64"), true, true, "2001:db8::1"},
		{"v6 only link-local keeps zone", addrs("192.0.2.1/24", "fe80::1/64"), true, true, "fe80::1%en0"},
		{"v6 skips loopback", addrs("::1/128", "fe80::1/64"), true, true, "fe80::1%en0"},
		{"v4 global", addrs("fe80::1/64", "169.254.1.1/16", "192.0.2.1/24"), false, true, "192.0.2.1"},
		{"v4 link-local without zone", addrs("169.254.1.1/16"), false, true, "169.254.1.1"},
		{"v4 loopback allowed", addrs("127.0.0.1/8", "::1/128"), false, false, "127.0.0.1"},
		{"v4 loopback skipped", addrs("127.0.0.1/8"), false, true, ""},
		{"no address of the family", addrs("192.0.2.1/24"), true, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickInterfaceIP("en0", tc.addrs, tc.v6, tc.skipLoop)
			if tc.want == "" {
				if err == nil {
					t.Errorf("pickInterfaceIP() = %v, want an error", got)
				}
				return
			}
			if err != nil || got.String() != tc.want {
				t.Errorf("pickInterfaceIP() = %v, %v; want %s", got, err, tc.want)
			}
		})
	}
}

// The TCP source address is looked up in the target's address family.
func TestTCPLocalAddrFamily(t *testing.T) {
	orig := lookupInterfaceIP
	t.Cleanup(func() { lookupInterfaceIP = orig })
	var gotV6 bool
	lookupInterfaceIP = func(name string, v6, skipLoopback bool) (*net.IPAddr, error) {
		gotV6 = v6
		if v6 {
			return &net.IPAddr{IP: net.ParseIP("fe80::1"), Zone: name}, nil
		}
		return &net.IPAddr{IP: net.ParseIP("192.0.2.1")}, nil
	}
	p := NewTCPProber(&TCPConfig{SourceInterface: "en0"}, "tcp")

	a, err := p.localAddr("[2001:db8::1]:443")
	if err != nil || !gotV6 || a.String() != "[fe80::1%en0]:0" {
		t.Errorf("IPv6 target: localAddr() = %v, %v (v6 lookup %v)", a, err, gotV6)
	}
	a, err = p.localAddr("192.0.2.10:443")
	if err != nil || gotV6 || a.String() != "192.0.2.1:0" {
		t.Errorf("IPv4 target: localAddr() = %v, %v (v6 lookup %v)", a, err, gotV6)
	}
}

// A source interface that cannot be used fails the probe instead of being
// silently ignored.
func TestTCPProberBadSourceInterface(t *testing.T) {
	addr := startTCPServer(t)
	p := NewTCPProber(&TCPConfig{SourceInterface: "nonexistent-interface"}, "tcp")
	if err := p.Accept("tcp://" + addr); err != nil {
		t.Fatal(err)
	}
	events := make(chan *Event, 100)
	p.sendProbe(events, addr, time.Second)
	close(events)
	var results []*Event
	for e := range events {
		results = append(results, e)
	}
	last := results[len(results)-1]
	if last.Result != FAILED || !strings.Contains(last.Message, "nonexistent-interface") {
		t.Errorf("probe with a bad source interface = %+v, want FAILED", last)
	}
}
