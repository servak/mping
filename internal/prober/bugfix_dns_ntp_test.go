package prober

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// --- helpers ---

func listenUDPOrSkip(t *testing.T, addr string) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

// serveNTP answers each request with the packet built by build(req).
// A nil result means no reply.
func serveNTP(t *testing.T, pc net.PacketConn, build func(req []byte) []byte) {
	t.Helper()
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req := append([]byte(nil), buf[:n]...)
			if n < 48 {
				continue
			}
			if resp := build(req); resp != nil {
				_, _ = pc.WriteTo(resp, addr)
			}
		}
	}()
}

func putNTPTime(b []byte, t time.Time) {
	sec, frac := ntpTimeFromTime(t)
	binary.BigEndian.PutUint32(b[0:4], sec)
	binary.BigEndian.PutUint32(b[4:8], frac)
}

// goodNTPResponse is a valid server reply; tests mutate it to break one field.
func goodNTPResponse(req []byte, rx, tx time.Time) []byte {
	resp := make([]byte, 48)
	resp[0] = 0x1C // LI=0, VN=3, mode=4
	resp[1] = 2
	copy(resp[24:32], req[40:48])
	putNTPTime(resp[32:40], rx)
	putNTPTime(resp[40:48], tx)
	return resp
}

func probeNTP(t *testing.T, target string) *Event {
	t.Helper()
	p := NewNTPProber(&NTPConfig{Server: "127.0.0.1", Port: 123}, "ntp")
	if err := p.Accept(target); err != nil {
		t.Fatalf("Accept(%q): %v", target, err)
	}
	if len(p.targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(p.targets))
	}
	ch := make(chan *Event, 4)
	for key := range p.targets {
		p.sendProbe(ch, key, time.Second)
	}
	<-ch // SENT
	return <-ch
}

// --- NTP: IPv6 ---

func TestNTPParseTargetIPv6(t *testing.T) {
	p := NewNTPProber(&NTPConfig{Server: "default", Port: 123}, "ntp")
	tests := []struct {
		target string
		host   string
		port   int
	}{
		{"ntp://[::1]:1123", "::1", 1123},
		{"ntp://[::1]", "::1", 123},
		{"ntp://::1", "::1", 123},
		{"ntp:[2001:db8::1]:99", "2001:db8::1", 99},
	}
	for _, tt := range tests {
		host, port, err := p.parseTarget(tt.target)
		if err != nil {
			t.Errorf("%s: %v", tt.target, err)
			continue
		}
		if host != tt.host || port != tt.port {
			t.Errorf("%s: got %s %d, want %s %d", tt.target, host, port, tt.host, tt.port)
		}
	}
}

func TestNTPProbeIPv6(t *testing.T) {
	pc := listenUDPOrSkip(t, "[::1]:0")
	serveNTP(t, pc, func(req []byte) []byte {
		now := time.Now()
		return goodNTPResponse(req, now, now)
	})
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	ev := probeNTP(t, "ntp://[::1]:"+port)
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
}

// --- NTP: response validation ---

func TestNTPResponseValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(resp []byte)
		wantMsg string
	}{
		{"client mode", func(r []byte) { r[0] = 0x1B }, "mode"},
		{"kiss of death", func(r []byte) { r[1] = 0; copy(r[12:16], "RATE") }, "RATE"},
		{"kiss DENY", func(r []byte) { r[1] = 0; copy(r[12:16], "DENY") }, "DENY"},
		{"unsynchronized", func(r []byte) { r[0] = 0xDC }, "unsynchronized"},
		{"origin mismatch", func(r []byte) { r[27] ^= 0xFF }, "origin"},
		{"zero transmit", func(r []byte) { clear(r[40:48]) }, "transmit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := listenUDPOrSkip(t, "127.0.0.1:0")
			serveNTP(t, pc, func(req []byte) []byte {
				now := time.Now()
				resp := goodNTPResponse(req, now, now)
				tt.mutate(resp)
				return resp
			})
			ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
			if ev.Result != FAILED {
				t.Fatalf("expected FAILED, got %v", ev.Result)
			}
			if !strings.Contains(ev.Message, tt.wantMsg) {
				t.Errorf("message %q does not contain %q", ev.Message, tt.wantMsg)
			}
		})
	}
}

// --- NTP: short datagram ---

func TestNTPShortResponse(t *testing.T) {
	pc := listenUDPOrSkip(t, "127.0.0.1:0")
	serveNTP(t, pc, func(req []byte) []byte { return make([]byte, 20) })
	start := time.Now()
	ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
	if ev.Result != FAILED || !strings.Contains(ev.Message, "short") {
		t.Fatalf("expected FAILED short response, got %v %q", ev.Result, ev.Message)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("short datagram should fail immediately, took %v", time.Since(start))
	}
}

// --- NTP: offset math ---

func TestNTPOffsetUsesRFC5905(t *testing.T) {
	// Server clock is 2s ahead and reports T2 and T3 50ms apart. The old
	// formula (T3 - (T1+rtt/2)) would include the 50ms processing time.
	const ahead = 2 * time.Second
	pc := listenUDPOrSkip(t, "127.0.0.1:0")
	serveNTP(t, pc, func(req []byte) []byte {
		rx := time.Now().Add(ahead)
		time.Sleep(50 * time.Millisecond)
		return goodNTPResponse(req, rx, time.Now().Add(ahead))
	})
	ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
	off := time.Duration(ev.Details.NTP.Offset) * time.Microsecond
	if d := (off - ahead).Abs(); d > 10*time.Millisecond {
		t.Errorf("offset = %v, want about %v", off, ahead)
	}
	// Reported delay must exclude the server's 50ms processing time.
	if ev.Rtt >= 40*time.Millisecond {
		t.Errorf("delay = %v, should exclude server processing time", ev.Rtt)
	}
}

// --- NTP: resolved once at Accept ---

// --- NTP: resolved once at Accept ---

func TestNTPResolvedAtAccept(t *testing.T) {
	p := NewNTPProber(&NTPConfig{Server: "127.0.0.1", Port: 123}, "ntp")
	if err := p.Accept("ntp://localhost:12345"); err != nil {
		t.Fatal(err)
	}
	if got := p.resolved["localhost:12345"]; got == "" || strings.Contains(got, "localhost") {
		t.Errorf("expected resolved IP address, got %q", got)
	}
}

// --- NTP: era ---

func TestNTPTimeEraRollover(t *testing.T) {
	// 2040-01-01 is in NTP era 1 (seconds wrap past 2036-02-07).
	want := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	sec, frac := ntpTimeFromTime(want)
	if got := ntpTimeToTime(sec, frac); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// A date in era 0 still works.
	want = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	sec, frac = ntpTimeFromTime(want)
	if got := ntpTimeToTime(sec, frac); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

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
