//go:build e2e

package e2e

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Local servers for every probe type. They all listen on loopback with an
// ephemeral port and are shut down by t.Cleanup.

// newHTTPServer serves:
//
//	/          200 "ok"
//	/health    200 "healthy" only when the X-E2E header is "yes", else 403
//	/status/N  status N
func newHTTPServer(t *testing.T, tls bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-E2E") != "yes" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("healthy"))
	})
	mux.HandleFunc("/status/500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewUnstartedServer(mux)
	// Handshake errors are expected when verification is enabled
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	if tls {
		srv.StartTLS() // self-signed certificate
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	return srv
}

// newTCPServer accepts and immediately closes connections.
func newTCPServer(t *testing.T, network, addr string) net.Addr {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Fatalf("listen %s %s: %v", network, addr, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return ln.Addr()
}

// closedTCPPort returns a loopback port that nothing listens on.
func closedTCPPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// dnsZone answers A/AAAA for e2e.test. and NXDOMAIN for everything else.
func dnsZone(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	q := req.Question[0]
	if q.Name != "e2e.test." {
		m.Rcode = dns.RcodeNameError
		_ = w.WriteMsg(m)
		return
	}
	hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 60, Rrtype: q.Qtype}
	switch q.Qtype {
	case dns.TypeA:
		m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.IPv4(127, 0, 0, 1)})
	case dns.TypeAAAA:
		m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.IPv6loopback})
	}
	_ = w.WriteMsg(m)
}

// newDNSServer starts a DNS server over "udp" or "tcp" on 127.0.0.1.
func newDNSServer(t *testing.T, network string) string {
	t.Helper()
	srv := &dns.Server{Handler: dns.HandlerFunc(dnsZone)}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	switch network {
	case "udp":
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv.PacketConn = pc
	case "tcp":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv.Listener = ln
	}
	go func() { _ = srv.ActivateAndServe() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS server did not start")
	}
	t.Cleanup(func() { _ = srv.Shutdown() })
	if srv.PacketConn != nil {
		return srv.PacketConn.LocalAddr().String()
	}
	return srv.Listener.Addr().String()
}

// newNTPServer answers NTP client requests with the current time (stratum 2).
func newNTPServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			if n < 48 {
				continue
			}
			resp := make([]byte, 48)
			resp[0] = 0x1C                // LI=0, VN=3, mode=4 (server)
			resp[1] = 2                   // stratum
			resp[3] = 0xEC                // precision (~-20)
			copy(resp[24:32], buf[40:48]) // origin = client's transmit time
			now := ntpTimestamp(time.Now())
			copy(resp[16:24], now) // reference
			copy(resp[32:40], now) // receive
			copy(resp[40:48], now) // transmit
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func ntpTimestamp(t time.Time) []byte {
	const ntpEpochOffset = 2208988800
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], uint32(t.Unix()+ntpEpochOffset))
	binary.BigEndian.PutUint32(b[4:8], uint32((uint64(t.Nanosecond())<<32)/1e9))
	return b
}

// ipv6LoopbackAvailable reports whether ::1 can be bound on this host.
func ipv6LoopbackAvailable() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
