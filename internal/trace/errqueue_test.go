package trace

import (
	"net"
	"net/netip"
	"os"
	"slices"
	"syscall"
	"testing"
)

// queuedEcho is an ICMP error on the error queue of a datagram ICMP socket:
// the quote starts with the ICMP header of the echo request.
func queuedEcho(t *testing.T, v6 bool, id, seq int, typ, code byte, offender string) queuedError {
	t.Helper()
	origin := byte(soEEOriginICMP)
	if v6 {
		origin = soEEOriginICMP6
	}
	return queuedError{
		payload: echoRequest(t, v6, id, seq),
		data:    extendedErr(origin, typ, code, net.ParseIP(offender)),
		dst:     netip.MustParseAddrPort("192.0.2.10:0"),
		at:      testEpoch,
	}
}

func TestICMPErrQueueReply(t *testing.T) {
	for _, tt := range []struct {
		name      string
		v6        bool
		typ, code byte
		offender  string
		want      replyKind
	}{
		{"v4 time exceeded", false, 11, 0, "192.0.2.1", kindTimeExceed},
		{"v4 net unreachable", false, 3, 0, "192.0.2.1", kindUnreachable},
		{"v4 host unreachable", false, 3, 1, "192.0.2.1", kindUnreachable},
		{"v4 port unreachable", false, 3, 3, "192.0.2.10", kindUnreachable},
		{"v4 administratively prohibited", false, 3, 13, "192.0.2.1", kindUnreachable},
		{"v6 time exceeded", true, 3, 0, "2001:db8::1", kindTimeExceed},
		{"v6 no route", true, 1, 0, "2001:db8::1", kindUnreachable},
		{"v6 administratively prohibited", true, 1, 1, "2001:db8::1", kindUnreachable},
		{"v6 address unreachable", true, 1, 3, "2001:db8::1", kindUnreachable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := queuedEcho(t, tt.v6, 0x1234, 0xabcd, tt.typ, tt.code, tt.offender)
			e.payload = e.payload[:icmpEchoHdrLen] // the RFC 792 minimum quote
			r, from, err := icmpErrQueueReply(tt.v6, e)
			if err != nil {
				t.Fatal(err)
			}
			if r.kind != tt.want || r.id != 0x1234 || r.seq != 0xabcd || r.ttl != 0 || from != tt.offender {
				t.Errorf("icmpErrQueueReply() = %+v from %s, want kind %d of probe 0xabcd from %s", r, from, tt.want, tt.offender)
			}
		})
	}

	// Anything that is not an ICMP error about an echo request is ignored
	for _, v6 := range []bool{false, true} {
		typ, offender := byte(icmpv4TimeExceeded), "192.0.2.1"
		echoReply, otherEcho := byte(0), byte(128)
		if v6 {
			typ, offender = icmpv6TimeExceeded, "2001:db8::1"
			echoReply, otherEcho = 129, 8
		}
		for name, change := range map[string]func(e *queuedError){
			"no quote":              func(e *queuedError) { e.payload = nil },
			"truncated quote":       func(e *queuedError) { e.payload = e.payload[:icmpEchoHdrLen-1] },
			"quoted UDP payload":    func(e *queuedError) { e.payload = udpProbePayload(3) },
			"quoted echo reply":     func(e *queuedError) { e.payload[0] = echoReply },
			"echo of the other IP":  func(e *queuedError) { e.payload[0] = otherEcho },
			"no control message":    func(e *queuedError) { e.data = nil },
			"truncated extended":    func(e *queuedError) { e.data = e.data[:sockExtendedErrLen-1] },
			"no offender":           func(e *queuedError) { e.data = e.data[:sockExtendedErrLen] },
			"local error":           func(e *queuedError) { e.data[4] = 1 }, // SO_EE_ORIGIN_LOCAL
			"other ICMP error":      func(e *queuedError) { e.data[5] = 4 },
			"offender of no family": func(e *queuedError) { clear(e.data[sockExtendedErrLen:]) },
		} {
			e := queuedEcho(t, v6, 1, 2, typ, 0, offender)
			change(&e)
			if r, from, err := icmpErrQueueReply(v6, e); err == nil {
				t.Errorf("v6=%v %s: got %+v from %q, want an error", v6, name, r, from)
			}
		}
	}
}

func TestUDPErrQueueReply(t *testing.T) {
	tp := &udpTransport{dst: net.IPv4(192, 0, 2, 10), srcPort: 50000}
	te := extendedErr(soEEOriginICMP, icmpv4TimeExceeded, 0, net.ParseIP("192.0.2.1"))
	ours, dnat := netip.MustParseAddrPort("192.0.2.10:33440"), netip.MustParseAddrPort("198.51.100.1:33440")
	for _, tt := range []struct {
		name    string
		e       queuedError
		ok      bool
		wantTTL int
	}{
		{"quoted payload", queuedError{payload: udpProbePayload(7), dst: ours, data: te}, true, 7},
		{"quoted payload, destination not rewritten", queuedError{payload: udpProbePayload(7), dst: dnat, data: te}, true, 7},
		{"short quote", queuedError{dst: ours, data: te}, true, 0},
		{"short quote, another destination", queuedError{dst: dnat, data: te}, false, 0},
		{"unknown destination", queuedError{payload: udpProbePayload(7), data: te}, false, 0},
		{"not an ICMP error", queuedError{payload: udpProbePayload(7), dst: ours, data: te[:8]}, false, 0},
	} {
		r, from, ok := tp.errQueueReply(tt.e)
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.ok)
		}
		if ok && (r.kind != kindTimeExceed || r.seq != 33440 || r.id != 50000 || r.ttl != tt.wantTTL || from != "192.0.2.1") {
			t.Errorf("%s: %+v from %s", tt.name, r, from)
		}
	}
}

// script returns a read that yields the given errors one after another and
// is then empty.
func script(errs ...error) func() error {
	return func() error {
		if len(errs) == 0 {
			return syscall.EAGAIN
		}
		err := errs[0]
		errs = errs[1:]
		return err
	}
}

func TestQueuePoller(t *testing.T) {
	// drain reads until the poller wants to wait, like receiveQueues
	drain := func(t *testing.T, readErr, readData func() error) (got []msgQueue) {
		t.Helper()
		var p queuePoller
		for range 1000 {
			q, wait := p.next(readErr, readData)
			if wait {
				return got
			}
			if q != noQueue {
				got = append(got, q)
			}
		}
		t.Fatal("never waits for the socket")
		return nil
	}
	pending := []error{syscall.EHOSTUNREACH, syscall.ECONNREFUSED, syscall.EINTR}

	for _, tt := range []struct {
		name       string
		errs, data []error
		want       []msgQueue
	}{
		{name: "empty"},
		{name: "errors first", errs: []error{nil, nil}, data: []error{nil}, want: []msgQueue{errQueue, errQueue, dataQueue}},
		// A pending socket error is reported instead of a datagram that
		// is there: it must not be left unread, the wakeup was its own
		{name: "pending error hides data", data: append(slices.Clone(pending), nil, nil), want: []msgQueue{dataQueue, dataQueue}},
		{name: "pending error between data", data: []error{nil, syscall.EHOSTUNREACH, nil}, want: []msgQueue{dataQueue, dataQueue}},
		{name: "failing error queue", errs: []error{syscall.EINTR, nil}, data: []error{nil}, want: []msgQueue{errQueue, dataQueue}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := drain(t, script(tt.errs...), script(tt.data...)); !slices.Equal(got, tt.want) {
				t.Errorf("read from %v, want %v", got, tt.want)
			}
		})
	}

	// A socket that fails for good neither spins nor starves the other queue
	broken := func() error { return os.NewSyscallError("recvmsg", syscall.EBADF) }
	if got := drain(t, broken, script(nil, nil)); !slices.Equal(got, []msgQueue{dataQueue, dataQueue}) {
		t.Errorf("broken error queue: read from %v", got)
	}
	if got := drain(t, script(nil), broken); !slices.Equal(got, []msgQueue{errQueue}) {
		t.Errorf("broken receive queue: read from %v", got)
	}
}

// A send that reports the ICMP error of an earlier probe sent nothing and is
// repeated; other failures are not.
func TestRetrySend(t *testing.T) {
	pending := sendErrno(syscall.EHOSTUNREACH)
	for _, tt := range []struct {
		name  string
		errs  []error
		want  error
		calls int
	}{
		{"sent", []error{nil}, nil, 1},
		{"pending errors", []error{pending, sendErrno(syscall.ECONNREFUSED), nil}, nil, 3},
		{"keeps failing", []error{pending, pending, pending, pending, nil}, syscall.EHOSTUNREACH, 1 + sendRetries},
		{"local failure", []error{sendErrno(syscall.ENOBUFS), nil}, syscall.ENOBUFS, 1},
	} {
		calls := 0
		send := script(tt.errs...)
		err := retrySend(func() error { calls++; return send() })
		if calls != tt.calls || (tt.want == nil) != (err == nil) || (tt.want != nil && !isErrno(err, []syscall.Errno{tt.want.(syscall.Errno)})) {
			t.Errorf("%s: %v after %d calls, want %v after %d", tt.name, err, calls, tt.want, tt.calls)
		}
	}
}
