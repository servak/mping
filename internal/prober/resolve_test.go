package prober

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// testResolver is a *net.Resolver that asks a DNS server on loopback, whatever
// the system is configured to ask.
func testResolver(t *testing.T, records ...string) *net.Resolver {
	t.Helper()
	answers := make(map[string][]dns.RR) // "name type"
	names := make(map[string]bool)
	for _, s := range records {
		rr, err := dns.NewRR(s)
		if err != nil {
			t.Fatal(err)
		}
		k := fmt.Sprint(rr.Header().Name, rr.Header().Rrtype)
		answers[k] = append(answers[k], rr)
		names[rr.Header().Name] = true
	}
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		m := new(dns.Msg).SetReply(q)
		m.Answer = answers[fmt.Sprint(q.Question[0].Name, q.Question[0].Qtype)]
		if !names[q.Question[0].Name] {
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	})}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", conn.LocalAddr().String())
		},
	}
}

// The lookups with a context yield what the functions of package net that they
// replace do, errors included.
func TestResolveLikeNet(t *testing.T) {
	r := testResolver(t,
		"v4.test. A 192.0.2.1", "v4.test. A 192.0.2.2",
		"v6.test. AAAA 2001:db8::1", "v6.test. AAAA 2001:db8::2",
		"both.test. A 192.0.2.3", "both.test. AAAA 2001:db8::3",
		"any6.test. AAAA ::", // takes a detour in package net
		"linklocal.test. AAAA fe80::1",
		"mapped.test. AAAA ::ffff:192.0.2.4",
		"empty.test. TXT x",
	)
	orig := net.DefaultResolver
	net.DefaultResolver = r
	t.Cleanup(func() { net.DefaultResolver = orig })

	ctx := context.Background()
	type result struct {
		addr any
		err  string
	}
	res := func(addr any, err error) result {
		if err != nil {
			return result{err: err.Error()}
		}
		return result{addr: addr}
	}
	for _, host := range []string{
		"v4.test.", "v6.test.", "both.test.", "any6.test.", "linklocal.test.", "mapped.test.", "empty.test.", "none.test.",
		"localhost", "", "127.0.0.1", "0.0.0.0", "::1", "::", "::ffff:192.0.2.1", "fe80::1%lo0", "fe80::1%nosuch",
		"127.0.0.1%lo0", "fe80::1%", "[::1]", "127.0.0.01", "127.1", "256.0.0.1", "::g", "a b", "a\x00b", "\xff", ".", "..",
		strings.Repeat("a", 300),
	} {
		t.Run(host, func(t *testing.T) {
			for _, network := range []string{"ip", "ip4", "ip6"} {
				want := res(net.ResolveIPAddr(network, host))
				if got := res(resolveIPAddr(ctx, r, network, host)); !reflect.DeepEqual(got, want) {
					t.Errorf("resolveIPAddr(%s) = %v, want %v", network, got, want)
				}
			}
			address := net.JoinHostPort(host, "123")
			want := res(net.ResolveUDPAddr("udp", address))
			if got := res(resolveUDPAddr(ctx, r, address)); !reflect.DeepEqual(got, want) {
				t.Errorf("resolveUDPAddr() = %v, want %v", got, want)
			}
			ips, err := net.LookupIP(host)
			want = res(nil, err)
			if err == nil {
				want = res(ips[0], nil)
			}
			if got := res(lookupFirstIP(ctx, r, host)); !reflect.DeepEqual(got, want) {
				t.Errorf("lookupFirstIP() = %v, want %v", got, want)
			}
		})
	}
}

// fakeResolver answers with lookup, for either family.
type fakeResolver struct {
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
}

func (r *fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.lookup(ctx, host)
}

// LookupIP drops the zones, like that of *net.Resolver.
func (r *fakeResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	addrs, err := r.lookup(ctx, host)
	var ips []net.IP
	for _, a := range addrs {
		v4 := a.IP.To4() != nil
		if network == "ip" || (network == "ip4") == v4 {
			ips = append(ips, a.IP)
		}
	}
	return ips, err
}

// addrResolver answers from a map; other names do not exist.
func addrResolver(addrs map[string][]string) *fakeResolver {
	return &fakeResolver{lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
		var ips []net.IPAddr
		for _, s := range addrs[host] {
			ip, zone, _ := strings.Cut(s, "%")
			ips = append(ips, net.IPAddr{IP: net.ParseIP(ip), Zone: zone})
		}
		if len(ips) == 0 {
			return nil, errNoSuchHost
		}
		return ips, nil
	}}
}

var errNoSuchHost = errors.New("no such host (test)")

// noResolver fails the test when anything is looked up.
func noResolver(t *testing.T) *fakeResolver {
	return &fakeResolver{lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
		t.Errorf("%q was looked up", host)
		return nil, errNoSuchHost
	}}
}

// A name that resolves to a zoned address keeps the zone, except for TCP.
func TestResolveZones(t *testing.T) {
	ctx := context.Background()
	r := addrResolver(map[string][]string{"ll": {"2001:db8::1", "fe80::1%eth1", "fe80::2%eth2"}, "ll1": {"fe80::2%eth2", "fe80::1%eth1"}})
	for host, want := range map[string]string{"ll": "2001:db8::1", "ll1": "fe80::2%eth2"} {
		for _, network := range []string{"ip", "ip6"} {
			if a, err := resolveIPAddr(ctx, r, network, host); err != nil || a.String() != want {
				t.Errorf("resolveIPAddr(%s, %s) = %v, %v, want %s", network, host, a, err, want)
			}
		}
		if a, err := resolveUDPAddr(ctx, r, host+":123"); err != nil || a.String() != net.JoinHostPort(want, "123") {
			t.Errorf("resolveUDPAddr(%s) = %v, %v, want %s", host, a, err, want)
		}
	}
	if ip, err := lookupFirstIP(ctx, r, "ll1"); err != nil || ip.String() != "fe80::2" {
		t.Errorf("lookupFirstIP() = %v, %v", ip, err)
	}
}

// Addresses, with or without a zone, and the empty host are not looked up.
func TestResolveLooksUpNamesOnly(t *testing.T) {
	ctx := context.Background()
	r := noResolver(t)
	for _, host := range []string{"", "127.0.0.1", "::1", "fe80::1%eth0", "::ffff:192.0.2.1"} {
		for _, network := range []string{"ip", "ip4", "ip6"} {
			_, _ = resolveIPAddr(ctx, r, network, host)
		}
		_, _ = resolveUDPAddr(ctx, r, net.JoinHostPort(host, "123"))
		_, _ = lookupFirstIP(ctx, r, host)
	}
}

// A resolver that finds no address but reports no error is survived.
func TestResolveNoAddress(t *testing.T) {
	ctx := context.Background()
	r := &fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }}
	if a, err := resolveIPAddr(ctx, r, "ip", "x"); err == nil {
		t.Errorf("resolveIPAddr() = %v", a)
	}
	if a, err := resolveUDPAddr(ctx, r, "x:1"); err == nil {
		t.Errorf("resolveUDPAddr() = %v", a)
	}
	if a, err := lookupFirstIP(ctx, r, "x"); err == nil {
		t.Errorf("lookupFirstIP() = %v", a)
	}
}
