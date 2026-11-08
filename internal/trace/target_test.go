package trace

import (
	"net"
	"runtime"
	"testing"
)

func TestHostFromTarget(t *testing.T) {
	tests := []struct {
		in       string
		host     string
		preferV6 bool
	}{
		{"8.8.8.8", "8.8.8.8", false},
		{"example.com", "example.com", false},
		{"example.com(93.184.216.34)", "93.184.216.34", false},
		{"example.com(2606:2800:220:1::1)", "2606:2800:220:1::1", true},
		{"icmpv4://example.com", "example.com", false},
		{"icmpv6://example.com", "example.com", true},
		{"icmpv6:example.com", "example.com", true},
		{"tcp://example.com:443", "example.com", false},
		{"tcp://[2001:db8::1]:443", "2001:db8::1", false},
		{"https://example.com/health?x=1", "example.com", false},
		{"http://127.0.0.1:8080/", "127.0.0.1", false},
		{"dns://8.8.8.8/example.com/A", "8.8.8.8", false},
		{"dns://1.1.1.1:5353/example.com", "1.1.1.1", false},
		{"ntp://pool.ntp.org", "pool.ntp.org", false},
		{"2001:db8::1", "2001:db8::1", false},
		{"my-ping://10.0.0.1", "10.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			host, v6 := HostFromTarget(tt.in)
			if host != tt.host || v6 != tt.preferV6 {
				t.Errorf("HostFromTarget(%q) = (%q, %v), want (%q, %v)", tt.in, host, v6, tt.host, tt.preferV6)
			}
		})
	}
}

func TestResolveLiteral(t *testing.T) {
	ip, err := Resolve("192.0.2.1", true)
	if err != nil || ip.String() != "192.0.2.1" {
		t.Errorf("Resolve literal = %v, %v", ip, err)
	}
}

func TestProtoForProbeType(t *testing.T) {
	for pt, want := range map[string]Proto{
		"": ProtoICMP, "icmpv4": ProtoICMP, "icmpv6": ProtoICMP,
		"http": ProtoUDP, "https": ProtoUDP, "tcp": ProtoUDP, "dns": ProtoUDP, "ntp": ProtoUDP,
	} {
		if got := ProtoForProbeType(pt); got != want {
			t.Errorf("ProtoForProbeType(%q) = %s, want %s", pt, got, want)
		}
	}
}

func TestResolveKeepsZone(t *testing.T) {
	ifname := loopbackName(t)
	addr, err := ResolveAddr("fe80::1%"+ifname, false)
	if err != nil {
		t.Fatalf("ResolveAddr: %v", err)
	}
	if !addr.IP.Equal(net.ParseIP("fe80::1")) || addr.Zone != ifname {
		t.Errorf("ResolveAddr = %+v, want fe80::1 zone %s", addr, ifname)
	}
	for _, target := range []string{"fe80::1%" + ifname, "tcp://[fe80::1%25" + ifname + "]:443"} {
		tr, err := New(target, Config{Proto: ProtoUDP, NoDNS: true})
		if err != nil {
			t.Logf("%s: cannot open sockets: %v", target, err)
			continue
		}
		tp := tr.tp.(*udpTransport)
		if tp.zone != ifname {
			t.Errorf("%s: transport zone = %q, want %q", target, tp.zone, ifname)
		}
		// macOS has fe80::1 on lo0: a send without the zone fails there
		if runtime.GOOS == "darwin" {
			if err := tp.send(1, UDPBasePort); err != nil {
				t.Errorf("%s: send: %v", target, err)
			}
		}
		tr.Close()
	}
}

func loopbackName(t *testing.T) string {
	t.Helper()
	ifs, err := net.Interfaces()
	if err != nil {
		t.Skip(err)
	}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagLoopback != 0 {
			return ifi.Name
		}
	}
	t.Skip("no loopback interface")
	return ""
}
