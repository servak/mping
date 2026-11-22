package prober

import (
	"errors"
	"net"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/servak/mping/internal/icmpid"
)

// fakeListener opens fake sockets, or fails with the error of the network.
type fakeListener struct {
	errs  map[string]error
	conns []packetConn // returned in this order, then new ones
	calls []string
}

func (l *fakeListener) listen(network, address string) (packetConn, error) {
	l.calls = append(l.calls, network+" "+address)
	if err := l.errs[network]; err != nil {
		return nil, err
	}
	if len(l.conns) > 0 {
		c := l.conns[0]
		l.conns = l.conns[1:]
		return c, nil
	}
	return newFakeICMPConn(), nil
}

func TestOpenICMPSocket(t *testing.T) {
	sock := func(call string, errno syscall.Errno) error { return os.NewSyscallError(call, errno) }
	denied := sock("socket", syscall.EACCES)
	notPermitted := &net.OpError{Op: "listen", Net: "ip4:icmp", Err: sock("socket", syscall.EPERM)}
	noAddr := sock("bind", syscall.EADDRNOTAVAIL)
	const (
		sysctl = `sysctl -w net.ipv4.ping_group_range="0 2147483647"`
		setcap = "setcap cap_net_raw=+ep"
		source = "source_interface"
	)

	cases := []struct {
		name  string
		goos  string
		typ   ProbeType
		errs  map[string]error
		calls []string // default: datagram, then raw
		// on success
		want    socketType
		checkID bool
		// on failure
		wantErrs []error
		text     []string
		notText  []string
	}{
		{
			name: "linux datagram", goos: "linux", typ: ICMPV4,
			calls: []string{"udp4 src"}, want: datagramSocket, checkID: false,
		},
		{
			name: "linux datagram v6", goos: "linux", typ: ICMPV6,
			calls: []string{"udp6 src"}, want: datagramSocket, checkID: false,
		},
		{
			name: "macOS datagram", goos: "darwin", typ: ICMPV4,
			calls: []string{"udp4 src"}, want: datagramSocket, checkID: true,
		},
		{
			name: "outside of ping_group_range", goos: "linux", typ: ICMPV4,
			errs: map[string]error{"udp4": denied}, want: rawSocket, checkID: true,
		},
		{
			name: "outside of ping_group_range v6", goos: "linux", typ: ICMPV6,
			errs:  map[string]error{"udp6": denied},
			calls: []string{"udp6 src", "ip6:ipv6-icmp src"}, want: rawSocket, checkID: true,
		},
		{
			name: "blocked by a sandbox", goos: "darwin", typ: ICMPV4,
			errs: map[string]error{"udp4": sock("socket", syscall.EPERM)}, want: rawSocket, checkID: true,
		},
		{
			name: "kernel without ping sockets", goos: "linux", typ: ICMPV4,
			errs: map[string]error{"udp4": sock("socket", syscall.EPROTONOSUPPORT)}, want: rawSocket, checkID: true,
		},
		{
			name: "socket type not supported", goos: "linux", typ: ICMPV4,
			errs: map[string]error{"udp4": sock("socket", syscall.ESOCKTNOSUPPORT)}, want: rawSocket, checkID: true,
		},
		{
			name: "no datagram ICMP on Windows", goos: "windows", typ: ICMPV4,
			calls: []string{"ip4:icmp src"}, want: rawSocket, checkID: true,
		},

		// Failures that raw sockets share are not hidden by the fallback
		{
			name: "source address not available", goos: "linux", typ: ICMPV4,
			errs: map[string]error{"udp4": noAddr}, calls: []string{"udp4 src"},
			wantErrs: []error{syscall.EADDRNOTAVAIL}, text: []string{"src", source},
			notText: []string{sysctl, setcap, "raw"},
		},
		{
			name: "no IPv6", goos: "linux", typ: ICMPV6,
			errs: map[string]error{"udp6": sock("socket", syscall.EAFNOSUPPORT)}, calls: []string{"udp6 src"},
			wantErrs: []error{syscall.EAFNOSUPPORT}, notText: []string{sysctl, setcap, source},
		},
		{
			name: "too many open files", goos: "darwin", typ: ICMPV4,
			errs: map[string]error{"udp4": sock("socket", syscall.EMFILE)}, calls: []string{"udp4 src"},
			wantErrs: []error{syscall.EMFILE}, notText: []string{"root"},
		},

		// Both fail: both causes and the advice for the platform
		{
			name: "linux without any permission", goos: "linux", typ: ICMPV4,
			errs:     map[string]error{"udp4": denied, "ip4:icmp": notPermitted},
			wantErrs: []error{syscall.EACCES, syscall.EPERM},
			text:     []string{"datagram: " + denied.Error(), "raw: " + notPermitted.Error(), sysctl, setcap},
			notText:  []string{source},
		},
		{
			name: "macOS without any permission", goos: "darwin", typ: ICMPV4,
			errs:     map[string]error{"udp4": denied, "ip4:icmp": notPermitted},
			wantErrs: []error{syscall.EACCES, syscall.EPERM},
			text:     []string{"datagram: ", "raw: ", "root"},
			notText:  []string{"sysctl", "setcap", "Administrator"},
		},
		{
			name: "raw socket with a bad source address", goos: "linux", typ: ICMPV4,
			errs:     map[string]error{"udp4": denied, "ip4:icmp": noAddr},
			wantErrs: []error{syscall.EACCES, syscall.EADDRNOTAVAIL},
			text:     []string{"datagram: ", "raw: ", source},
			notText:  []string{"sysctl", "setcap"},
		},
		{
			name: "windows without permission", goos: "windows", typ: ICMPV4,
			errs: map[string]error{"ip4:icmp": sock("socket", 10013)}, calls: []string{"ip4:icmp src"},
			wantErrs: []error{syscall.Errno(10013)}, text: []string{"Administrator"},
			notText: []string{"sysctl", "setcap", "datagram"},
		},
		{
			name: "windows other failure", goos: "windows", typ: ICMPV4,
			errs: map[string]error{"ip4:icmp": errors.New("boom")}, calls: []string{"ip4:icmp src"},
			text: []string{"boom"}, notText: []string{"Administrator", "datagram"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeListener{errs: tc.errs}
			s, err := openICMPSocket(tc.goos, tc.typ, "src", l.listen)
			wantCalls := tc.calls
			if wantCalls == nil {
				wantCalls = []string{"udp4 src", "ip4:icmp src"}
			}
			if !slices.Equal(l.calls, wantCalls) {
				t.Errorf("sockets tried = %q, want %q", l.calls, wantCalls)
			}
			if tc.want != "" {
				if err != nil {
					t.Fatalf("error = %v, want a %s socket", err, tc.want)
				}
				defer s.lease.Release()
				if s.typ != tc.want || s.checkID != tc.checkID {
					t.Errorf("got a %s socket with checkID=%v, want %s, %v", s.typ, s.checkID, tc.want, tc.checkID)
				}
				if _, ok := s.conn.(datagramConn); ok != (tc.want == datagramSocket) {
					t.Errorf("connection is a %T", s.conn)
				}
				return
			}
			if err == nil {
				t.Fatalf("got a %s socket, want an error", s.typ)
			}
			t.Log(err)
			for _, want := range tc.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("error %q does not wrap %q", err, want)
				}
			}
			for _, want := range tc.text {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			for _, not := range tc.notText {
				if strings.Contains(err.Error(), not) {
					t.Errorf("error %q contains %q", err, not)
				}
			}
		})
	}
}

// portConn is a datagram socket to which the kernel gave an echo ID.
type portConn struct {
	*fakeICMPConn
	id uint16
}

func (c portConn) LocalAddr() net.Addr { return &net.UDPAddr{Port: int(c.id)} }

// The echo ID Linux chose for a datagram socket is taken out of circulation,
// so that a path tracer on a raw socket does not get the same one; a socket
// whose ID a tracer already holds is replaced. Elsewhere the ID is our own.
func TestOpenDatagramReservesKernelID(t *testing.T) {
	tracer, err := icmpid.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer tracer.Release()
	// an ID far away from the allocator's cursor
	free := tracer.ID() ^ 0x8000
	if free == 0 {
		free = 1
	}

	clash := portConn{newFakeICMPConn(), tracer.ID()}
	l := &fakeListener{conns: []packetConn{clash, portConn{newFakeICMPConn(), free}}}
	s, err := openICMPSocket("linux", ICMPV4, "0.0.0.0", l.listen)
	if err != nil {
		t.Fatal(err)
	}
	if s.lease.ID() != free || s.checkID {
		t.Errorf("lease = %#x (checkID %v), want the kernel's %#x, unchecked", s.lease.ID(), s.checkID, free)
	}
	if !clash.isClosed() {
		t.Error("the socket with the tracer's ID was not closed")
	}
	if _, ok := icmpid.Reserve(free); ok {
		t.Error("the ID of the open socket is still free")
	}
	s.lease.Release()

	// A clash that persists does not keep the prober from starting
	l = &fakeListener{}
	for range maxReserveAttempts {
		l.conns = append(l.conns, portConn{newFakeICMPConn(), tracer.ID()})
	}
	s, err = openICMPSocket("linux", ICMPV4, "0.0.0.0", l.listen)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.calls) != maxReserveAttempts || s.lease.ID() == tracer.ID() {
		t.Errorf("%d sockets opened, lease %#x", len(l.calls), s.lease.ID())
	}
	s.lease.Release()

	// macOS datagram sockets have no such ID, whatever the local port says
	l = &fakeListener{conns: []packetConn{portConn{newFakeICMPConn(), tracer.ID()}}}
	s, err = openICMPSocket("darwin", ICMPV4, "0.0.0.0", l.listen)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.calls) != 1 || s.lease.ID() == tracer.ID() || !s.checkID {
		t.Errorf("macOS: %d sockets opened, lease %#x, checkID %v", len(l.calls), s.lease.ID(), s.checkID)
	}
	s.lease.Release()
}

// addrConn records the address written to and returns packets from peer.
type addrConn struct {
	wrote net.Addr
	peer  net.Addr
	err   error
}

func (c *addrConn) ReadFrom([]byte) (int, net.Addr, error) { return 3, c.peer, c.err }
func (c *addrConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	c.wrote = dst
	return len(b), c.err
}
func (c *addrConn) Close() error { return nil }

type addrTTLConn struct{ *addrConn }

func (c addrTTLConn) ReadFromTTL([]byte) (int, int, net.Addr, error) { return 3, 57, c.peer, c.err }

// Datagram sockets speak *net.UDPAddr; the prober speaks *net.IPAddr, whose
// String is a target key: without a port and with the IPv6 zone.
func TestDatagramConnAddresses(t *testing.T) {
	for _, key := range []string{"192.0.2.1", "2001:db8::1", "::1", "fe80::1%en0"} {
		t.Run(key, func(t *testing.T) {
			ip, zone, _ := strings.Cut(key, "%")
			target := &net.IPAddr{IP: net.ParseIP(ip), Zone: zone}
			udp := &net.UDPAddr{IP: target.IP, Zone: zone}
			inner := &addrConn{peer: udp}

			for _, c := range []datagramConn{{inner}, {addrTTLConn{inner}}} {
				if _, err := c.WriteTo([]byte{1}, target); err != nil {
					t.Fatal(err)
				}
				if got, ok := inner.wrote.(*net.UDPAddr); !ok || got.String() != udp.String() {
					t.Errorf("WriteTo passed on %T %v, want *net.UDPAddr %v", inner.wrote, inner.wrote, udp)
				}
				if n, src, err := c.ReadFrom(nil); n != 3 || err != nil || src.String() != key {
					t.Errorf("ReadFrom() = %d, %v, %v; want source %s", n, src, err, key)
				}
				wantTTL := 0
				if _, ok := c.packetConn.(ttlPacketConn); ok {
					wantTTL = 57
				}
				if n, ttl, src, err := c.ReadFromTTL(nil); n != 3 || ttl != wantTTL || err != nil || src.String() != key {
					t.Errorf("ReadFromTTL() = %d, %d, %v, %v; want TTL %d and source %s", n, ttl, src, err, wantTTL, key)
				}
			}
		})
	}

	failing := datagramConn{&addrConn{err: net.ErrClosed}}
	if _, src, err := failing.ReadFrom(nil); src != nil || !errors.Is(err, net.ErrClosed) {
		t.Errorf("ReadFrom() on a closed socket = %v, %v", src, err)
	}
	if _, _, src, err := failing.ReadFromTTL(nil); src != nil || !errors.Is(err, net.ErrClosed) {
		t.Errorf("ReadFromTTL() on a closed socket = %v, %v", src, err)
	}
	if _, err := failing.WriteTo(nil, &net.IPAddr{}); !errors.Is(err, net.ErrClosed) {
		t.Errorf("WriteTo() on a closed socket = %v", err)
	}
}

// A prober on a datagram socket counts replies whose echo ID was rewritten
// by the kernel where that happens (Linux), and only there.
func TestICMPProberRewrittenEchoID(t *testing.T) {
	for goos, want := range map[string]reason{"linux": SUCCESS, "darwin": TIMEOUT} {
		t.Run(goos, func(t *testing.T) {
			conn := newFakeICMPConn()
			conn.rewriteID = true
			l := &fakeListener{conns: []packetConn{conn}}
			s, err := openICMPSocket(goos, ICMPV4, "0.0.0.0", l.listen)
			if err != nil {
				t.Fatal(err)
			}
			p := newICMPProber(ICMPV4, s, []byte("x"), "icmpv4")
			if err := p.Accept("icmpv4://127.0.0.1"); err != nil {
				t.Fatal(err)
			}
			log, stop := startFake(t, p, 20*time.Millisecond, 50*time.Millisecond)
			waitFor(t, "rounds", func() bool { return log.count(want) >= 3 })
			stop()
			if n := log.count(want); n != log.count(SENT) {
				t.Errorf("%d of %d rounds ended in %v", n, log.count(SENT), want)
			}
		})
	}
}
