package prober

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// --- DNS ---

func TestDNSParseTargetValidation(t *testing.T) {
	cfg := &DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}
	bad := map[string]string{
		"dns://127.0.0.1:abc/x.test":   "invalid port",
		"dns://127.0.0.1:0/x.test":     "invalid port",
		"dns://127.0.0.1:70000/x.test": "invalid port",
		"dns://127.0.0.1/x.test/BOGUS": "record type",
	}
	for target, want := range bad {
		err := NewDNSProber(cfg, "dns").Accept(target)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected error containing %q, got %v", target, want, err)
		}
	}
}

func TestDNSAcceptIPv6Literal(t *testing.T) {
	p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}, "dns")
	for _, target := range []string{"dns://[::1]/x.test", "dns://[::1]:5353/x.test", "dns://::1/x.test"} {
		if err := p.Accept(target); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
}

func dnsProbe(t *testing.T, target string) *Event {
	t.Helper()
	p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}, "dns")
	if err := p.Accept(target); err != nil {
		t.Fatalf("Accept(%q): %v", target, err)
	}
	ch := make(chan *Event, 4)
	p.sendProbe(ch, p.targets[0], 2*time.Second)
	<-ch
	return <-ch
}

func aRecordReply(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Answer = append(m.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.IPv4(192, 0, 2, 1),
	})
	return m
}

func startDNS(t *testing.T, network, addr string, h dns.Handler) net.Addr {
	t.Helper()
	started := make(chan struct{})
	srv := &dns.Server{Net: network, Addr: addr, Handler: h, NotifyStartedFunc: func() { close(started) }}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case <-started:
	case err := <-errc:
		t.Skipf("cannot start DNS server on %s: %v", addr, err)
	case <-time.After(5 * time.Second):
		t.Fatal("DNS server did not start")
	}
	t.Cleanup(func() { _ = srv.Shutdown() })
	if network == "tcp" {
		return srv.Listener.Addr()
	}
	return srv.PacketConn.LocalAddr()
}

func TestDNSProbeIPv6Server(t *testing.T) {
	addr := startDNS(t, "udp", "[::1]:0", dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		_ = w.WriteMsg(aRecordReply(r))
	}))
	_, port, _ := net.SplitHostPort(addr.String())
	ev := dnsProbe(t, "dns://[::1]:"+port+"/v6.test")
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
}

func TestDNSTruncatedRetriesOverTCP(t *testing.T) {
	h := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := aRecordReply(r)
		if _, isUDP := w.RemoteAddr().(*net.UDPAddr); isUDP {
			m.Answer = nil
			m.Truncated = true
		}
		_ = w.WriteMsg(m)
	})
	// Bind UDP first, then TCP on the same port.
	udp := startDNS(t, "udp", "127.0.0.1:0", h)
	startDNS(t, "tcp", udp.String(), h)
	ev := dnsProbe(t, "dns://"+udp.String()+"/tc.test")
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
	if ev.Details.DNS.AnswerCount != 1 {
		t.Errorf("expected full answer over TCP, got %d answers", ev.Details.DNS.AnswerCount)
	}
}

func TestDNSTruncatedTCPUnavailableFails(t *testing.T) {
	udp := startDNS(t, "udp", "127.0.0.1:0", dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Truncated = true
		_ = w.WriteMsg(m)
	}))
	ev := dnsProbe(t, "dns://"+udp.String()+"/tc.test")
	if ev.Result == SUCCESS {
		t.Fatal("truncated answer with no TCP fallback must not be SUCCESS")
	}
}

func TestDNSRecordTypesShared(t *testing.T) {
	cfg := &DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}
	// Valid for dns.StringToType but not in the supported list.
	if err := NewDNSProber(cfg, "dns").Accept("dns://127.0.0.1/x.test/DNSKEY"); err == nil {
		t.Error("DNSKEY should be rejected by Accept")
	}
	for _, rt := range supportedDNSRecordTypes {
		c := *cfg
		c.RecordType = strings.ToLower(rt)
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%s): %v", rt, err)
		}
		p := NewDNSProber(cfg, "dns")
		if err := p.Accept("dns://127.0.0.1/x.test/" + strings.ToLower(rt)); err != nil {
			t.Errorf("Accept(%s): %v", rt, err)
		} else if p.targets[0].RecordType != rt {
			t.Errorf("record type = %q, want %q", p.targets[0].RecordType, rt)
		}
	}
}

func slowTruncatingServers(t *testing.T, udpDelay, tcpDelay time.Duration) string {
	t.Helper()
	h := func(delay time.Duration) dns.HandlerFunc {
		return func(w dns.ResponseWriter, r *dns.Msg) {
			time.Sleep(delay)
			m := aRecordReply(r)
			if _, isUDP := w.RemoteAddr().(*net.UDPAddr); isUDP {
				m.Answer = nil
				m.Truncated = true
			}
			_ = w.WriteMsg(m)
		}
	}
	udp := startDNS(t, "udp", "127.0.0.1:0", h(udpDelay))
	startDNS(t, "tcp", udp.String(), h(tcpDelay))
	return udp.String()
}

// The TCP retry only gets the remaining budget: the probe stays within the
// timeout and a slow retry is a TIMEOUT even though its error is wrapped.
func TestDNSTruncatedRetryUsesRemainingBudget(t *testing.T) {
	addr := slowTruncatingServers(t, 300*time.Millisecond, 400*time.Millisecond)
	p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A"}, "dns")
	if err := p.Accept("dns://" + addr + "/tc.test"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Event, 4)
	start := time.Now()
	p.sendProbe(ch, p.targets[0], 500*time.Millisecond)
	elapsed := time.Since(start)
	<-ch
	ev := <-ch
	if ev.Result != TIMEOUT {
		t.Errorf("result = %v (%s), want TIMEOUT", ev.Result, ev.Message)
	}
	if elapsed > 650*time.Millisecond {
		t.Errorf("probe took %v, should stay near the 500ms timeout", elapsed)
	}
}

// RTT of a UDP+TCP probe is the total wall time.
func TestDNSTruncatedRTTIsTotalElapsed(t *testing.T) {
	addr := slowTruncatingServers(t, 100*time.Millisecond, 100*time.Millisecond)
	ev := dnsProbe(t, "dns://"+addr+"/tc.test")
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
	if ev.Rtt < 190*time.Millisecond || ev.Rtt > time.Second {
		t.Errorf("rtt = %v, want about 200ms+ (total elapsed)", ev.Rtt)
	}
}

func TestDNSErrorEventTimeoutThroughWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", os.ErrDeadlineExceeded))
	if ev := dnsErrorEvent("k", time.Now(), err); ev.Result != TIMEOUT {
		t.Errorf("wrapped timeout classified %v", ev.Result)
	}
	if ev := dnsErrorEvent("k", time.Now(), errors.New("boom")); ev.Result != FAILED {
		t.Errorf("plain error classified %v", ev.Result)
	}
}

// With UseTCP set the query goes straight over TCP; the TC bit is not retried.
func TestDNSUseTCPPath(t *testing.T) {
	h := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) { _ = w.WriteMsg(aRecordReply(r)) })
	udp := startDNS(t, "udp", "127.0.0.1:0", dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		t.Error("UDP server must not be queried when use_tcp is set")
	}))
	startDNS(t, "tcp", udp.String(), h)
	p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A", UseTCP: true}, "dns")
	if err := p.Accept("dns://" + udp.String() + "/tcp.test"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Event, 4)
	p.sendProbe(ch, p.targets[0], 2*time.Second)
	<-ch
	ev := <-ch
	if ev.Result != SUCCESS || !ev.Details.DNS.UseTCP || ev.Details.DNS.AnswerCount != 1 {
		t.Fatalf("got %v %s %+v", ev.Result, ev.Message, ev.Details)
	}
}

func TestDNSUseTCPTruncatedNotRetried(t *testing.T) {
	tcp := startDNS(t, "tcp", "127.0.0.1:0", dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Truncated = true
		_ = w.WriteMsg(m)
	}))
	p := NewDNSProber(&DNSConfig{Server: "127.0.0.1", Port: 53, RecordType: "A", UseTCP: true}, "dns")
	if err := p.Accept("dns://" + tcp.String() + "/tc.test"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Event, 4)
	p.sendProbe(ch, p.targets[0], 2*time.Second)
	<-ch
	if ev := <-ch; ev.Result != SUCCESS && ev.Result != FAILED {
		t.Errorf("unexpected result %v", ev.Result)
	}
}
