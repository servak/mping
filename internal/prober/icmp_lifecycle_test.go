package prober

import (
	"errors"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// fakeICMPConn answers every echo request it is asked to send, after first
// delivering a read error and an unparsable packet, which the prober must
// survive. It stands in for the raw socket, which unit tests cannot open.
type fakeICMPConn struct {
	pkts      chan fakePacket
	closed    chan struct{}
	closeOnce sync.Once
	noReply   bool
	// replyDelay delays every reply, to simulate a slow path
	replyDelay time.Duration
	// net, when set, delivers every reply to all of its connections, as
	// the kernel does for raw ICMP sockets
	net *fakeICMPNet
}

// fakeICMPNet is a host on which every raw ICMP socket sees every reply.
type fakeICMPNet struct {
	mu    sync.Mutex
	conns []*fakeICMPConn
}

func (n *fakeICMPNet) newConn() *fakeICMPConn {
	c := newFakeICMPConn()
	c.net = n
	n.mu.Lock()
	n.conns = append(n.conns, c)
	n.mu.Unlock()
	return c
}

func (n *fakeICMPNet) deliver(pk fakePacket) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, c := range n.conns {
		select {
		case c.pkts <- pk:
		default: // a stalled reader drops the packet
		}
	}
}

type fakePacket struct {
	data []byte
	addr net.Addr
	err  error
}

func newFakeICMPConn() *fakeICMPConn {
	c := &fakeICMPConn{pkts: make(chan fakePacket, 100), closed: make(chan struct{})}
	c.pkts <- fakePacket{err: errors.New("transient read error")}
	c.pkts <- fakePacket{data: []byte{0xff}, addr: &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}}
	return c
}

func (c *fakeICMPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case <-c.closed:
		return 0, nil, &net.OpError{Op: "read", Net: "ip4:icmp", Err: net.ErrClosed}
	case pk := <-c.pkts:
		if pk.err != nil {
			return 0, nil, pk.err
		}
		return copy(b, pk.data), pk.addr, nil
	}
}

func (c *fakeICMPConn) WriteTo(b []byte, dst net.Addr) (int, error) {
	select {
	case <-c.closed:
		return 0, &net.OpError{Op: "write", Net: "ip4:icmp", Err: net.ErrClosed}
	default:
	}
	if c.noReply {
		return len(b), nil
	}
	req, err := icmp.ParseMessage(1, b)
	if err != nil {
		return 0, err
	}
	reply := icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: req.Body}
	data, err := reply.Marshal(nil)
	if err != nil {
		return 0, err
	}
	pk := fakePacket{data: data, addr: dst}
	send := func() {
		if c.net != nil {
			c.net.deliver(pk)
			return
		}
		select {
		case c.pkts <- pk:
		default:
		}
	}
	if c.replyDelay > 0 {
		time.AfterFunc(c.replyDelay, send)
	} else {
		send()
	}
	return len(b), nil
}

func (c *fakeICMPConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeICMPConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func newFakeICMPProber(t *testing.T, conn *fakeICMPConn) *ICMPProber {
	t.Helper()
	p := newTestProber(t, ICMPV4, conn, []byte("fake"), "icmpv4")
	if err := p.Accept("icmpv4://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestICMPProberLifecycleFakeConn(t *testing.T) {
	base := runtime.NumGoroutine()
	conn := newFakeICMPConn()
	p := newFakeICMPProber(t, conn)

	res := runLifecycle(t, p, []string{"127.0.0.1"}, lifecycleInterval, 200*time.Millisecond)
	for _, e := range res.events {
		if e.Result == SUCCESS && (e.Details == nil || e.Details.ICMP == nil || e.Details.ICMP.Payload != "fake") {
			t.Errorf("SUCCESS without the expected ICMP details: %+v", e)
		}
	}
	if !conn.isClosed() {
		t.Error("Stop did not close the connection")
	}
	if goroutineRunning("(*ICMPProber).recvPkts") {
		t.Error("receive goroutine still running after Stop")
	}
	stopReturns(t, p)
	waitGoroutines(t, base)
}

// Unanswered requests are reported as TIMEOUT, also for the rounds still
// outstanding when Stop is called.
func TestICMPProberTimeoutFakeConn(t *testing.T) {
	conn := newFakeICMPConn()
	conn.noReply = true
	p := newFakeICMPProber(t, conn)

	events := make(chan *Event, 1000)
	log := collectEvents(events)
	done := make(chan error, 1)
	go func() { done <- p.Start(events, lifecycleInterval, 100*time.Millisecond) }()
	deadline := time.Now().Add(5 * time.Second)
	for log.count(TIMEOUT) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no TIMEOUT; events: %s", describeEvents(log.snapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopReturns(t, p)
	if err := <-done; err != nil {
		t.Errorf("Start() error = %v", err)
	}
	close(events)
	<-log.done

	if sent, timeouts := log.count(SENT), log.count(TIMEOUT); sent != timeouts {
		t.Errorf("every request must time out: %d SENT, %d TIMEOUT", sent, timeouts)
	}
}

func TestICMPProberStopBeforeStart(t *testing.T) {
	conn := newFakeICMPConn()
	p := newFakeICMPProber(t, conn)
	stopReturns(t, p)
	if !conn.isClosed() {
		t.Error("Stop before Start did not close the connection")
	}
	events := make(chan *Event, 10)
	if err := p.Start(events, lifecycleInterval, lifecycleTimeout); err != nil {
		t.Errorf("Start() after Stop error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Start after Stop emitted %d events", len(events))
	}
	stopReturns(t, p)
}

// The table helpers return their events instead of sending them, so the
// prober never blocks on the channel while holding its mutex.
func TestICMPProberTableEvents(t *testing.T) {
	p := newTestProber(t, ICMPV4, newFakeICMPConn(), nil, "icmpv4")
	p.targets["127.0.0.1"] = icmpTarget{key: "127.0.0.1", name: "127.0.0.1"}
	p.targets["127.0.0.2"] = icmpTarget{key: "127.0.0.2", name: "localhost(127.0.0.2)"}
	p.timeout = time.Hour
	p.addTable(1, time.Now().Add(-time.Millisecond))

	msg := icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{}}
	if e := p.success(1, "127.0.0.1", msg, nil, 0); e == nil || e.Result != SUCCESS || e.Rtt <= 0 {
		t.Errorf("success() = %+v, want SUCCESS", e)
	}
	if e := p.success(1, "127.0.0.1", msg, nil, 0); e != nil {
		t.Errorf("duplicate reply: success() = %+v, want nil", e)
	}
	if e := p.success(2, "127.0.0.1", msg, nil, 0); e != nil {
		t.Errorf("unknown round: success() = %+v, want nil", e)
	}
	if e := p.success(1, "192.0.2.1", msg, nil, 0); e != nil {
		t.Errorf("unknown address: success() = %+v, want nil", e)
	}
	if events, pending, next := p.checkTimeout(); len(events) != 0 || pending != 1 || next.IsZero() {
		t.Errorf("checkTimeout() before timeout = %d events, %d pending, next %v", len(events), pending, next)
	}

	p.timeout = 0
	events, pending, next := p.checkTimeout()
	if pending != 0 || len(events) != 1 || !next.IsZero() {
		t.Fatalf("checkTimeout() = %d events, %d pending; want 1, 0", len(events), pending)
	}
	if e := events[0]; e.Result != TIMEOUT || e.Key != "127.0.0.2" || e.DisplayName != "localhost(127.0.0.2)" {
		t.Errorf("timeout event = %+v", e)
	}

	p.addTable(3, time.Now())
	if e := p.failed(3, "127.0.0.1", errors.New("boom")); e == nil || e.Result != FAILED || e.Message != "boom" {
		t.Errorf("failed() = %+v, want FAILED", e)
	}
	if e := p.failed(4, "127.0.0.1", errors.New("boom")); e != nil {
		t.Errorf("unknown round: failed() = %+v, want nil", e)
	}
}

func TestICMPIPAddr(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1"} {
		a, err := ipAddr(s)
		if err != nil || a.String() != s {
			t.Errorf("ipAddr(%q) = %v, %v", s, a, err)
		}
	}
}
