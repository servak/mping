package prober

import (
	"testing"
	"time"
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

	pLost := newICMPProber(ICMPV4, lost, []byte("x"), "ttl1")
	if err := pLost.Accept("ttl1://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	pOK := newICMPProber(ICMPV4, ok, []byte("x"), "icmpv4")
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
			p := newICMPProber(ICMPV4, newFakeICMPConn(), nil, tc.prefix)
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
