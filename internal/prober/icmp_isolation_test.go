package prober

import (
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// startFake starts p on its own event channel and returns the log and a
// function that stops p and waits for Start to return.
func startFake(t *testing.T, p *ICMPProber, interval, timeout time.Duration) (*eventLog, func()) {
	t.Helper()
	events := make(chan *Event, 1000)
	log := collectEvents(events)
	done := make(chan error, 1)
	go func() { done <- p.Start(events, interval, timeout) }()
	return log, func() {
		t.Helper()
		stopReturns(t, p)
		if err := <-done; err != nil {
			t.Errorf("Start() error = %v", err)
		}
		close(events)
		<-log.done
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Raw ICMP sockets receive every echo reply on the host. A prober must only
// count the replies to its own requests, even when another prober in the
// process pings the same address with the same sequence numbers.
func TestICMPProbersDoNotShareReplies(t *testing.T) {
	var host fakeICMPNet
	lost := host.newConn()
	lost.noReply = true // e.g. TTL=1: the requests never reach the target
	ok := host.newConn()

	pLost := newTestProber(t, ICMPV4, lost, []byte("x"), "ttl1")
	if err := pLost.Accept("ttl1://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	pOK := newTestProber(t, ICMPV4, ok, []byte("x"), "icmpv4")
	if err := pOK.Accept("icmpv4://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if pLost.runID == pOK.runID {
		t.Errorf("probers share echo ID %d", pLost.runID)
	}

	logLost, stopLost := startFake(t, pLost, 20*time.Millisecond, 50*time.Millisecond)
	logOK, stopOK := startFake(t, pOK, 20*time.Millisecond, 50*time.Millisecond)
	waitFor(t, "rounds", func() bool { return logLost.count(TIMEOUT) >= 3 && logOK.count(SUCCESS) >= 3 })
	stopLost()
	stopOK()

	if n := logLost.count(SUCCESS); n != 0 {
		t.Errorf("prober without replies counted %d SUCCESS from the other prober's replies", n)
	}
	if n := logOK.count(TIMEOUT); n != 0 {
		t.Errorf("answered prober reported %d TIMEOUT", n)
	}
}

// The default ICMP probers key their targets by IP (as before); any other
// ICMP prober prefixes the key and the name with its own name, so that the
// same address probed by two probers gets two rows instead of one merged row.
func TestICMPProberKeys(t *testing.T) {
	cases := []struct {
		prefix, target, key, name string
	}{
		{"icmpv4", "icmpv4://127.0.0.1", "127.0.0.1", "127.0.0.1"},
		{"icmpv4", "icmpv4://localhost", "127.0.0.1", "localhost(127.0.0.1)"},
		{"ttl1", "ttl1://127.0.0.1", "ttl1://127.0.0.1", "ttl1://127.0.0.1"},
		{"ttl1", "ttl1:localhost", "ttl1://127.0.0.1", "ttl1://localhost(127.0.0.1)"},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			p := newTestProber(t, ICMPV4, newFakeICMPConn(), nil, tc.prefix)
			if err := p.Accept(tc.target); err != nil {
				t.Fatal(err)
			}
			log, stop := startFake(t, p, 10*time.Millisecond, 10*time.Millisecond)
			waitFor(t, "REGISTER", func() bool { return len(log.snapshot()) > 0 })
			stop()
			e := log.snapshot()[0]
			if e.Result != REGISTER || e.Key != tc.key || e.DisplayName != tc.name || e.ProbeType != ICMPV4 {
				t.Errorf("REGISTER = {Key:%q Name:%q Type:%q}, want {%q %q icmpv4}", e.Key, e.DisplayName, e.ProbeType, tc.key, tc.name)
			}
		})
	}
}

// A reply arriving after the timeout is a TIMEOUT, reported once, never a
// SUCCESS with an RTT above the timeout.
func TestICMPProberLateReply(t *testing.T) {
	p := newTestProber(t, ICMPV4, newFakeICMPConn(), nil, "icmpv4")
	if err := p.Accept("icmpv4://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	p.timeout = 50 * time.Millisecond
	p.addTable(1, time.Now().Add(-100*time.Millisecond))
	msg := icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{}}
	e := p.success(1, "127.0.0.1", msg, nil, 0)
	if e == nil || e.Result != TIMEOUT {
		t.Fatalf("late reply: success() = %+v, want TIMEOUT", e)
	}
	if events, pending, _ := p.checkTimeout(); len(events) != 0 || pending != 0 {
		t.Errorf("checkTimeout() after the late reply = %d events, %d pending; want 0, 0", len(events), pending)
	}
}

// The same through Start: with -i 1000 -t 50 and an RTT of 150ms every
// probe is a TIMEOUT.
func TestICMPProberLateReplyLifecycle(t *testing.T) {
	conn := newFakeICMPConn()
	conn.replyDelay = 150 * time.Millisecond
	p := newFakeICMPProber(t, conn)
	log, stop := startFake(t, p, time.Second, 50*time.Millisecond)
	waitFor(t, "TIMEOUT", func() bool { return log.count(TIMEOUT) > 0 })
	time.Sleep(200 * time.Millisecond) // let the late reply arrive
	stop()
	if n := log.count(SUCCESS); n != 0 {
		t.Errorf("late replies counted as %d SUCCESS: %s", n, describeEvents(log.snapshot()))
	}
	if sent, timeouts := log.count(SENT), log.count(TIMEOUT); sent != timeouts {
		t.Errorf("%d SENT, %d TIMEOUT; want one TIMEOUT per request", sent, timeouts)
	}
}

// The timeout is enforced on its own deadline, not at the next tick.
func TestICMPProberTimeoutBeforeNextTick(t *testing.T) {
	conn := newFakeICMPConn()
	conn.noReply = true
	p := newFakeICMPProber(t, conn)
	log, stop := startFake(t, p, time.Hour, 50*time.Millisecond)
	defer stop()
	waitFor(t, "TIMEOUT", func() bool { return log.count(TIMEOUT) > 0 })
}

// Stop does not wait for the next interval: it returns as soon as the last
// round is answered, or at the latest when its timeout expires.
func TestICMPProberStopDoesNotWaitInterval(t *testing.T) {
	for _, noReply := range []bool{false, true} {
		name := "answered"
		if noReply {
			name = "unanswered"
		}
		t.Run(name, func(t *testing.T) {
			conn := newFakeICMPConn()
			conn.noReply = noReply
			p := newFakeICMPProber(t, conn)
			const timeout = 300 * time.Millisecond
			log, stop := startFake(t, p, time.Hour, timeout)
			waitFor(t, "SENT", func() bool { return log.count(SENT) > 0 })
			if !noReply {
				waitFor(t, "SUCCESS", func() bool { return log.count(SUCCESS) > 0 })
			}
			begin := time.Now()
			stop()
			took := time.Since(begin)
			limit := timeout + 200*time.Millisecond
			if !noReply {
				limit = 200 * time.Millisecond // nothing left to wait for
			}
			if took > limit {
				t.Errorf("Stop took %v, want at most %v", took, limit)
			}
			if noReply && log.count(TIMEOUT) != log.count(SENT) {
				t.Errorf("%d SENT, %d TIMEOUT", log.count(SENT), log.count(TIMEOUT))
			}
		})
	}
}
