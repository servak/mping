package prober

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNTPConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *NTPConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   123,
			},
			wantErr: false,
		},
		{
			name: "valid config with max offset",
			config: &NTPConfig{
				Server:    "time.google.com",
				Port:      123,
				MaxOffset: 1000 * time.Millisecond,
			},
			wantErr: false,
		},
		{
			name: "invalid config - empty server",
			config: &NTPConfig{
				Server: "",
				Port:   123,
			},
			wantErr: true,
			errMsg:  "NTP server is required",
		},
		{
			name: "invalid config - invalid port 0",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   0,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
		},
		{
			name: "invalid config - invalid port negative",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   -1,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
		},
		{
			name: "invalid config - invalid port too large",
			config: &NTPConfig{
				Server: "pool.ntp.org",
				Port:   65536,
			},
			wantErr: true,
			errMsg:  "invalid NTP server port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

func TestNTPProberAccept(t *testing.T) {
	config := &NTPConfig{
		Server: "pool.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	tests := []struct {
		name    string
		target  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid new format",
			target:  "ntp://time.google.com",
			wantErr: false,
		},
		{
			name:    "valid new format with port",
			target:  "ntp://time.google.com:123",
			wantErr: false,
		},
		{
			name:    "valid legacy format",
			target:  "ntp:time.google.com",
			wantErr: false,
		},
		{
			name:    "valid legacy format with port",
			target:  "ntp:time.google.com:123",
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			target:  "http://time.google.com",
			wantErr: true,
			errMsg:  "not accepted",
		},
		{
			name:    "invalid port format",
			target:  "ntp://time.google.com:abc",
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name:    "invalid server:port format",
			target:  "ntp://[::1",
			wantErr: true,
			errMsg:  "invalid server:port format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := prober.Accept(tt.target)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestNTPProberParseTarget(t *testing.T) {
	config := &NTPConfig{
		Server: "default.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	tests := []struct {
		name       string
		target     string
		wantServer string
		wantPort   int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "new format - server only",
			target:     "ntp://time.google.com",
			wantServer: "time.google.com",
			wantPort:   123, // default port
			wantErr:    false,
		},
		{
			name:       "new format - server with port",
			target:     "ntp://time.google.com:1123",
			wantServer: "time.google.com",
			wantPort:   1123,
			wantErr:    false,
		},
		{
			name:       "legacy format - server only",
			target:     "ntp:time.google.com",
			wantServer: "time.google.com",
			wantPort:   123, // default port
			wantErr:    false,
		},
		{
			name:       "legacy format - server with port",
			target:     "ntp:time.google.com:1123",
			wantServer: "time.google.com",
			wantPort:   1123,
			wantErr:    false,
		},
		{
			name:       "empty target - use config defaults",
			target:     "ntp://",
			wantServer: "default.ntp.org",
			wantPort:   123,
			wantErr:    false,
		},
		{
			name:    "invalid port",
			target:  "ntp://time.google.com:abc",
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name:    "invalid server:port format",
			target:  "ntp://[::1",
			wantErr: true,
			errMsg:  "invalid server:port format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, port, err := prober.parseTarget(tt.target)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if server != tt.wantServer {
					t.Errorf("Expected server '%s', got '%s'", tt.wantServer, server)
				}
				if port != tt.wantPort {
					t.Errorf("Expected port %d, got %d", tt.wantPort, port)
				}
			}
		})
	}
}

func TestNTPTimeConversion(t *testing.T) {
	// Test time conversion functions
	now := time.Now()

	// Convert to NTP format and back
	sec, frac := ntpTimeFromTime(now)
	converted := ntpTimeToTime(sec, frac)

	// Allow small difference due to precision loss
	diff := converted.Sub(now).Abs()
	if diff > time.Microsecond {
		t.Errorf("Time conversion precision loss too large: %v", diff)
	}
}

func TestNTPProberRegistration(t *testing.T) {
	config := &NTPConfig{
		Server: "pool.ntp.org",
		Port:   123,
	}
	prober := NewNTPProber(config, "ntp")

	// Add some targets
	targets := []string{
		"ntp://time.google.com",
		"ntp://pool.ntp.org:123",
	}

	for _, target := range targets {
		err := prober.Accept(target)
		if err != nil {
			t.Errorf("Failed to accept target %s: %v", target, err)
		}
	}

	// Test registration events
	events := make(chan *Event, 10)
	for serverAddr, tgt := range prober.targets {
		register(events, serverAddr, tgt.display, NTP)
	}
	close(events)

	// Count registration events
	count := 0
	for event := range events {
		if event.Result != REGISTER {
			t.Errorf("Expected REGISTER event, got %v", event.Result)
		}
		count++
	}

	if count != len(targets) {
		t.Errorf("Expected %d registration events, got %d", len(targets), count)
	}
}

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

func TestNTPEraPivotBoundaries(t *testing.T) {
	tests := []struct {
		name string
		sec  uint32
		want time.Time
	}{
		// Just below the pivot: era 1 (2104); at the pivot: era 0 (1968).
		{"2^31-1 is era 1", 1<<31 - 1, time.Unix(1<<31-1+1<<32-2208988800, 0)},
		{"2^31 is era 0", 1 << 31, time.Unix(1<<31-2208988800, 0)},
		{"era 0 ends 2036-02-07 06:28:15", 1<<32 - 1, time.Date(2036, 2, 7, 6, 28, 15, 0, time.UTC)},
		{"era 1 starts 2036-02-07 06:28:16", 0, time.Date(2036, 2, 7, 6, 28, 16, 0, time.UTC)},
	}
	for _, tt := range tests {
		if got := ntpTimeToTime(tt.sec, 0); !got.Equal(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestNTPReresolvesPerProbe: hostnames are looked up on every probe (so a
// removed pool member is not pinned) and IP literals never are.
func TestNTPReresolvesPerProbe(t *testing.T) {
	dead := listenUDPOrSkip(t, "127.0.0.1:0")
	live := listenUDPOrSkip(t, "127.0.0.1:0")
	dead.Close()
	serveNTP(t, live, func(req []byte) []byte {
		now := time.Now()
		return goodNTPResponse(req, now, now)
	})

	p := NewNTPProber(&NTPConfig{Server: "pool.test", Port: 123}, "ntp")
	calls := 0
	p.resolve = func(network, address string) (*net.UDPAddr, error) {
		calls++
		if calls <= 2 { // Accept + first probe: the pool member that later disappears
			return dead.LocalAddr().(*net.UDPAddr), nil
		}
		return live.LocalAddr().(*net.UDPAddr), nil
	}
	if err := p.Accept("ntp://pool.test"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Event, 8)
	p.sendProbe(ch, "pool.test:123", 300*time.Millisecond)
	<-ch
	if ev := <-ch; ev.Result == SUCCESS {
		t.Fatal("probe to the dead address should not succeed")
	}
	p.sendProbe(ch, "pool.test:123", time.Second)
	<-ch
	if ev := <-ch; ev.Result != SUCCESS {
		t.Fatalf("second probe should follow the new address, got %v: %s", ev.Result, ev.Message)
	}
	if calls != 3 {
		t.Errorf("resolve calls = %d, want 3 (Accept + 2 probes)", calls)
	}

	// IP literal: resolved at Accept only.
	lp := NewNTPProber(&NTPConfig{Server: "127.0.0.1", Port: 123}, "ntp")
	n := 0
	lp.resolve = func(network, address string) (*net.UDPAddr, error) {
		n++
		return net.ResolveUDPAddr(network, address)
	}
	if err := lp.Accept("ntp://" + live.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	lp.sendProbe(ch, live.LocalAddr().String(), time.Second)
	<-ch
	<-ch
	if n != 1 {
		t.Errorf("IP literal resolve calls = %d, want 1 (Accept only)", n)
	}
}

// TestNTPBareIPv6IsAddressOnly pins the documented convention: an unbracketed
// IPv6 literal never carries a port.
func TestNTPBareIPv6IsAddressOnly(t *testing.T) {
	p := NewNTPProber(&NTPConfig{Server: "default", Port: 123}, "ntp")
	host, port, err := p.parseTarget("ntp://2001:db8::1:123")
	if err != nil {
		t.Fatal(err)
	}
	if host != "2001:db8::1:123" || port != 123 {
		t.Errorf("got %s %d, want address 2001:db8::1:123 on default port 123", host, port)
	}
	h, ps, err := splitServerPort("2001:db8::1:123")
	if err != nil || h != "2001:db8::1:123" || ps != "" {
		t.Errorf("splitServerPort = %q %q %v", h, ps, err)
	}
}

func TestNTPKoDRequiresMatchingOrigin(t *testing.T) {
	pc := listenUDPOrSkip(t, "127.0.0.1:0")
	serveNTP(t, pc, func(req []byte) []byte {
		now := time.Now()
		resp := goodNTPResponse(req, now, now)
		resp[1] = 0
		copy(resp[12:16], "RATE")
		resp[27] ^= 0xFF // does not answer our request
		return resp
	})
	ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
	if ev.Result != FAILED || strings.Contains(ev.Message, "Kiss") || !strings.Contains(ev.Message, "origin") {
		t.Errorf("unmatched stratum-0 packet must be an origin error, got %v %q", ev.Result, ev.Message)
	}
}

func TestNTPRejectsStratum16(t *testing.T) {
	for _, stratum := range []byte{16, 17, 255} {
		pc := listenUDPOrSkip(t, "127.0.0.1:0")
		serveNTP(t, pc, func(req []byte) []byte {
			now := time.Now()
			resp := goodNTPResponse(req, now, now)
			resp[1] = stratum
			return resp
		})
		ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
		if ev.Result != FAILED || !strings.Contains(ev.Message, "stratum") {
			t.Errorf("stratum %d: got %v %q", stratum, ev.Result, ev.Message)
		}
	}
}

// TestNTPNegativeDelayNeverZero: stats treats RTT 0 as unset.
func TestNTPNegativeDelayNeverZero(t *testing.T) {
	pc := listenUDPOrSkip(t, "127.0.0.1:0")
	serveNTP(t, pc, func(req []byte) []byte {
		now := time.Now()
		// Server processing time (T3-T2) far above the real round trip.
		return goodNTPResponse(req, now, now.Add(time.Hour))
	})
	ev := probeNTP(t, "ntp://"+pc.LocalAddr().String())
	if ev.Result != SUCCESS {
		t.Fatalf("expected SUCCESS, got %v: %s", ev.Result, ev.Message)
	}
	if ev.Rtt <= 0 {
		t.Errorf("rtt = %v, want > 0", ev.Rtt)
	}
}
