package prober

import (
	"net"
	"testing"
	"time"
)

// Two spellings of the same ip:port must stay two targets with their own names.
func TestTCPProberSameAddressKeepsBothTargets(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	a := "tcp://127.0.0.1:" + port
	b := "tcp://[::ffff:127.0.0.1]:" + port
	p := NewTCPProber(&TCPConfig{}, "tcp")
	for _, tg := range []string{a, b} {
		if err := p.Accept(tg); err != nil {
			t.Fatalf("Accept(%s): %v", tg, err)
		}
	}

	ch := make(chan *Event, 64)
	go p.Start(ch, time.Hour, time.Second)
	defer p.Stop()

	names := map[string]string{} // key -> name, from REGISTER
	success := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(success) < 2 {
		select {
		case e := <-ch:
			switch e.Result {
			case REGISTER:
				names[e.Key] = e.DisplayName
			case SUCCESS:
				success[e.DisplayName] = true
			}
		case <-deadline:
			t.Fatalf("timeout; registered=%v success=%v", names, success)
		}
	}
	if len(names) != 2 {
		t.Fatalf("registered %d targets (%v), want 2", len(names), names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen[a] || !seen[b] {
		t.Errorf("display names = %v, want both %q and %q", names, a, b)
	}
}
