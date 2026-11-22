package trace

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/servak/mping/internal/icmpid"
	"github.com/servak/mping/internal/icmpsock"
)

// fakeICMPConn is an ICMP socket on a path of one router and the destination
// behind it. It answers probes like a Linux datagram socket with an error
// queue, or on the receive queue only.
type fakeICMPConn struct {
	network string
	port    int   // the echo ID the kernel chose
	recvErr error // of enableRecvErr

	mu        sync.Mutex
	ttl       int
	sendErrs  []error // reported by the next sends
	errs      chan queuedError
	data      chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeICMPSock(network string, port int) *fakeICMPConn {
	return &fakeICMPConn{network: network, port: port,
		errs: make(chan queuedError, 64), data: make(chan []byte, 64), closed: make(chan struct{})}
}

func (c *fakeICMPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case d := <-c.data:
		return copy(b, d), &net.IPAddr{IP: net.IPv4(192, 0, 2, 10)}, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *fakeICMPConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sendErrs) > 0 {
		err := c.sendErrs[0]
		c.sendErrs = c.sendErrs[1:]
		return 0, err
	}
	m, err := icmp.ParseMessage(protocolICMP, b)
	if err != nil {
		return 0, err
	}
	echo := m.Body.(*icmp.Echo)
	echo.ID = c.port // rewritten by the kernel
	if c.ttl == 1 {
		q, _ := m.Marshal(nil)
		c.errs <- queuedError{payload: q, at: time.Now(),
			data: extendedErr(soEEOriginICMP, icmpv4TimeExceeded, 0, net.ParseIP("192.0.2.1"))}
	} else {
		m.Type = ipv4.ICMPTypeEchoReply
		r, _ := m.Marshal(nil)
		c.data <- r
	}
	return len(b), nil
}

func (c *fakeICMPConn) LocalAddr() net.Addr {
	if strings.HasPrefix(c.network, "udp") {
		return &net.UDPAddr{Port: c.port}
	}
	return &net.IPAddr{}
}

func (c *fakeICMPConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeICMPConn) wasClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *fakeICMPConn) setTTL(ttl int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
	return nil
}

func (c *fakeICMPConn) enableRecvErr() error { return c.recvErr }

func (c *fakeICMPConn) receiveQueues(onErr func(queuedError), onData func([]byte, string, time.Time)) {
	for {
		select {
		case e := <-c.errs:
			onErr(e)
		case d := <-c.data:
			onData(d, "192.0.2.10", time.Now())
		case <-c.closed:
			return
		}
	}
}

// localIDs makes the transports of a test use their own allocator.
func localIDs(t *testing.T) *icmpid.Allocator {
	alloc, old := icmpid.New(1, 0, nil), echoIDs
	echoIDs = alloc
	t.Cleanup(func() { echoIDs = old })
	return alloc
}

func TestOpenICMPTransport(t *testing.T) {
	sock := func(e syscall.Errno) error { return os.NewSyscallError("socket", e) }
	denied := sock(syscall.EACCES)
	notPermitted := &net.OpError{Op: "listen", Net: "ip4:icmp", Err: sock(syscall.EPERM)}
	const (
		sysctl = `sysctl -w net.ipv4.ping_group_range="0 2147483647"`
		setcap = "setcap cap_net_raw=+ep"
	)
	for _, tt := range []struct {
		name    string
		goos    string
		v6      bool
		errs    map[string]error
		recvErr error
		calls   []string
		// on success
		raw, errQueue, destOnly, checkID bool
		// on failure
		wantErrs      []error
		text, notText []string
	}{
		// Linux prefers datagram sockets, with or without privileges
		{name: "linux datagram", goos: "linux", calls: []string{"udp4"}, errQueue: true},
		{name: "linux datagram v6", goos: "linux", v6: true, calls: []string{"udp6"}, errQueue: true},
		{name: "outside of ping_group_range", goos: "linux", errs: map[string]error{"udp4": denied},
			calls: []string{"udp4", "ip4:icmp"}, raw: true, checkID: true},
		{name: "outside of ping_group_range v6", goos: "linux", v6: true, errs: map[string]error{"udp6": denied},
			calls: []string{"udp6", "ip6:ipv6-icmp"}, raw: true, checkID: true},
		{name: "kernel without ping sockets", goos: "linux", errs: map[string]error{"udp4": sock(syscall.EPROTONOSUPPORT)},
			calls: []string{"udp4", "ip4:icmp"}, raw: true, checkID: true},

		// Without an error queue raw sockets see more
		{name: "no error queue, raw", goos: "linux", recvErr: sock(syscall.ENOPROTOOPT),
			calls: []string{"udp4", "ip4:icmp"}, raw: true, checkID: true},
		{name: "no error queue, no raw", goos: "linux", recvErr: sock(syscall.ENOPROTOOPT), errs: map[string]error{"ip4:icmp": notPermitted},
			calls: []string{"udp4", "ip4:icmp"}, destOnly: true},

		// Failures raw sockets share are not hidden by the fallback
		{name: "no IPv6", goos: "linux", v6: true, errs: map[string]error{"udp6": sock(syscall.EAFNOSUPPORT)},
			calls: []string{"udp6"}, wantErrs: []error{syscall.EAFNOSUPPORT}, notText: []string{sysctl, setcap}},
		{name: "too many open files", goos: "linux", errs: map[string]error{"udp4": sock(syscall.EMFILE)},
			calls: []string{"udp4"}, wantErrs: []error{syscall.EMFILE}, notText: []string{sysctl, setcap}},
		{name: "linux without any permission", goos: "linux", errs: map[string]error{"udp4": denied, "ip4:icmp": notPermitted},
			calls: []string{"udp4", "ip4:icmp"}, wantErrs: []error{syscall.EACCES, syscall.EPERM}, text: []string{"datagram: ", "raw: ", sysctl, setcap}},

		// macOS prefers raw sockets and needs no error queue
		{name: "macOS root", goos: "darwin", calls: []string{"ip4:icmp"}, raw: true, checkID: true},
		{name: "macOS", goos: "darwin", errs: map[string]error{"ip4:icmp": notPermitted}, recvErr: errUDPUnsupported,
			calls: []string{"ip4:icmp", "udp4"}, checkID: true},
		{name: "macOS v6", goos: "darwin", v6: true, errs: map[string]error{"ip6:ipv6-icmp": notPermitted}, recvErr: errUDPUnsupported,
			calls: []string{"ip6:ipv6-icmp", "udp6"}, checkID: true},
		{name: "macOS without any permission", goos: "darwin", errs: map[string]error{"udp4": denied, "ip4:icmp": notPermitted},
			calls: []string{"ip4:icmp", "udp4"}, wantErrs: []error{syscall.EACCES, syscall.EPERM}, text: []string{"run as root"}, notText: []string{sysctl, setcap}},

		{name: "windows", goos: "windows", calls: []string{"ip4:icmp"}, raw: true, checkID: true},
		{name: "windows without permission", goos: "windows", errs: map[string]error{"ip4:icmp": sock(10013)},
			calls: []string{"ip4:icmp"}, wantErrs: []error{syscall.Errno(10013)}, text: []string{"Administrator"}, notText: []string{"datagram"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := localIDs(t)
			var calls []string
			var conns []*fakeICMPConn
			tp, err := openICMPTransport(tt.goos, &net.IPAddr{IP: net.IPv4(192, 0, 2, 10)}, tt.v6,
				func(network, address string) (icmpConn, error) {
					calls = append(calls, network)
					if err := tt.errs[network]; err != nil {
						return nil, err
					}
					c := newFakeICMPSock(network, 4711)
					c.recvErr = tt.recvErr
					conns = append(conns, c)
					return c, nil
				})
			if !slices.Equal(calls, tt.calls) {
				t.Errorf("sockets tried = %q, want %q", calls, tt.calls)
			}
			if len(tt.wantErrs) > 0 {
				if err == nil {
					t.Fatalf("got %+v, want an error", tp)
				}
				for _, want := range tt.wantErrs {
					if !errors.Is(err, want) {
						t.Errorf("error %q does not wrap %q", err, want)
					}
				}
				for _, want := range tt.text {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				for _, not := range tt.notText {
					if strings.Contains(err.Error(), not) {
						t.Errorf("error %q contains %q", err, not)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tp.raw != tt.raw || tp.errQueue != tt.errQueue || tp.destOnly != tt.destOnly || tp.checkID != tt.checkID {
				t.Errorf("raw=%v errQueue=%v destOnly=%v checkID=%v, want %v %v %v %v", tp.raw, tp.errQueue, tp.destOnly, tp.checkID,
					tt.raw, tt.errQueue, tt.destOnly, tt.checkID)
			}
			if tp.privileged() != tt.raw || tp.hidesHops() != tt.destOnly {
				t.Errorf("privileged()=%v hidesHops()=%v", tp.privileged(), tp.hidesHops())
			}
			if _, ok := tp.dstAddr.(*net.IPAddr); ok != tt.raw {
				t.Errorf("destination is a %T", tp.dstAddr)
			}
			// On Linux datagram sockets the ID is the kernel's
			if kernel := tt.goos == "linux" && !tt.raw; (tp.id == 4711) != kernel || tp.lease.ID() != tp.id {
				t.Errorf("echo ID %d (lease %d)", tp.id, tp.lease.ID())
			}
			_ = tp.close()
			for _, c := range conns {
				if !c.wasClosed() {
					t.Errorf("%s socket left open", c.network)
				}
			}
			if l, ok := alloc.Reserve(tp.id); !ok {
				t.Error("echo ID not released")
			} else {
				l.Release()
			}
		})
	}
}

// A tracer whose kernel-chosen ID a raw socket of the process holds already
// gets another socket.
func TestICMPTransportReservesKernelID(t *testing.T) {
	alloc := localIDs(t)
	raw, ok := alloc.Reserve(4711)
	if !ok {
		t.Fatal("ID taken")
	}
	defer raw.Release()
	clash, free := newFakeICMPSock("udp4", 4711), newFakeICMPSock("udp4", 4712)
	conns := []*fakeICMPConn{clash, free}
	tp, err := openICMPTransport("linux", &net.IPAddr{IP: net.IPv4(192, 0, 2, 10)}, false,
		func(string, string) (icmpConn, error) {
			c := conns[0]
			conns = conns[1:]
			return c, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer tp.close()
	if tp.id != 4712 || !clash.wasClosed() || free.wasClosed() {
		t.Errorf("echo ID %d, clashing socket closed: %v", tp.id, clash.wasClosed())
	}
	if _, ok := alloc.Reserve(4712); ok {
		t.Error("the ID of the open socket is still free")
	}
}

func linuxDatagramTransport(t *testing.T, c *fakeICMPConn) *icmpTransport {
	t.Helper()
	localIDs(t)
	tp, err := openICMPTransport("linux", &net.IPAddr{IP: net.IPv4(192, 0, 2, 10)}, false,
		func(string, string) (icmpConn, error) { return c, nil })
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

// A trace on a Linux datagram socket shows the router from the error queue
// and the destination from the receive queue, and pending socket errors
// neither lose a probe nor end the trace.
func TestICMPTraceOnErrorQueue(t *testing.T) {
	c := newFakeICMPSock("udp4", 4711)
	c.sendErrs = []error{sendErrno(syscall.EHOSTUNREACH), sendErrno(syscall.ECONNREFUSED)}
	tp := linuxDatagramTransport(t, c)
	tr := newTracer(Config{Interval: 5 * time.Millisecond, Timeout: 50 * time.Millisecond, NoDNS: true},
		net.IPv4(192, 0, 2, 10), tp)
	if err := tr.Run(context.Background(), 4); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	res := tr.Snapshot()
	if res.Privileged || res.HopsHidden || !res.Reached || len(res.Hops) != 2 {
		t.Fatalf("result = %+v", res)
	}
	for i, want := range []string{"192.0.2.1", "192.0.2.10"} {
		if h := res.Hops[i]; h.Addr != want || h.Sent != 4 || h.Recv != 4 || len(h.Responders) != 1 {
			t.Errorf("hop %d = %+v, want %s without loss", i+1, h, want)
		}
	}
	if !c.wasClosed() {
		t.Error("socket left open")
	}
}

// Without an error queue the trace still ends at the destination, and says
// that it cannot see the hops.
func TestICMPTraceWithoutErrorQueue(t *testing.T) {
	c := newFakeICMPSock("udp4", 4711)
	c.recvErr = syscall.ENOPROTOOPT
	localIDs(t)
	tp, err := openICMPTransport("linux", &net.IPAddr{IP: net.IPv4(192, 0, 2, 10)}, false,
		func(network, _ string) (icmpConn, error) {
			if network != "udp4" {
				return nil, syscall.EPERM
			}
			return c, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	tr := newTracer(Config{Interval: 5 * time.Millisecond, Timeout: 20 * time.Millisecond, NoDNS: true},
		net.IPv4(192, 0, 2, 10), tp)
	if err := tr.Run(context.Background(), 3); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	res := tr.Snapshot()
	if res.Privileged || !res.HopsHidden || !res.Reached || len(res.Hops) != 2 || res.Hops[0].Addr != "" || res.Hops[1].Addr != "192.0.2.10" {
		t.Errorf("result = %+v", res)
	}
}

// Replies on the error queue of a datagram socket carry the kernel's echo ID
// and are not compared with ours.
func TestICMPTransportErrorQueueID(t *testing.T) {
	c := newFakeICMPSock("udp4", 4711)
	tp := linuxDatagramTransport(t, c)
	c.errs <- queuedError{payload: []byte("not an echo request")}
	c.errs <- queuedEcho(t, false, 9999, 7, 3, 1, "192.0.2.1")
	got := make(chan reply, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tp.receive(func(r reply, from string, at time.Time) {
			if from != "192.0.2.1" || !at.Equal(testEpoch) {
				t.Errorf("reply from %s at %v", from, at)
			}
			got <- r
		})
	}()
	select {
	case r := <-got:
		if r.kind != kindUnreachable || r.seq != 7 {
			t.Errorf("reply = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Error("no reply delivered")
	}
	_ = tp.close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("receive still running after close")
	}
}

// The real socket of a datagram trace gives access to its error queue (on
// Linux), and a closed one ends the receive loop.
func TestICMPTransportCloseEndsReceive(t *testing.T) {
	for _, dst := range []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback} {
		v6 := dst.To4() == nil
		tp, err := newICMPTransport(&net.IPAddr{IP: dst}, v6)
		if err != nil {
			t.Logf("no ICMP socket for %v: %v", dst, err)
			continue
		}
		if tp.hidesHops() {
			t.Errorf("%v: intermediate hops cannot be seen", dst)
		}
		if nc, ok := tp.conn.(netConn); !ok {
			t.Errorf("connection is a %T", tp.conn)
		} else if _, err := nc.socket(); err != nil {
			t.Error(err)
		}
		if !tp.raw && !tp.checkID && tp.id != icmpsock.KernelEchoID(tp.conn) {
			t.Errorf("echo ID %d, the kernel's is %d", tp.id, icmpsock.KernelEchoID(tp.conn))
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			tp.receive(func(reply, string, time.Time) {})
		}()
		time.Sleep(20 * time.Millisecond)
		_ = tp.close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: receive still running after close", dst)
		}
	}
}
