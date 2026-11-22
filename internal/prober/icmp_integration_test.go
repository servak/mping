//go:build integration

package prober

// ICMP probers on real sockets against loopback. Run with:
//
//	go test -tags integration -run Integration -v ./internal/prober/
//
// Environment (same conventions as internal/trace):
//
//	MPING_IT_STRICT     "1" fails instead of skipping when no ICMP socket
//	                    can be opened
//	MPING_IT_PRIVILEGED "1" requires the raw socket, "0" the datagram one.
//	                    The prober only falls back to raw where datagram
//	                    ICMP is not available, so "1" needs
//	                    net.ipv4.ping_group_range="1 0" and cap_net_raw on
//	                    Linux, and cannot be had on macOS.

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/servak/mping/internal/icmpid"
)

// integrationProber opens a prober on a real socket of the type that
// MPING_IT_PRIVILEGED asks for.
func integrationProber(t *testing.T, version ProbeType, cfg *ICMPConfig, prefix string) *ICMPProber {
	t.Helper()
	p, err := NewICMPProber(version, cfg, prefix)
	if err != nil {
		if os.Getenv("MPING_IT_STRICT") == "1" {
			t.Fatalf("NewICMPProber: %v", err)
		}
		t.Skipf("cannot open an ICMP socket: %v", err)
	}
	t.Cleanup(p.Stop)
	want := map[string]socketType{"0": datagramSocket, "1": rawSocket}[os.Getenv("MPING_IT_PRIVILEGED")]
	if want != "" && p.socket != want {
		t.Fatalf("prober uses a %s socket, MPING_IT_PRIVILEGED requires %s", p.socket, want)
	}
	t.Logf("%s: %s socket, echo ID %#x (checked: %v)", prefix, p.socket, p.runID, p.checkID)
	if p.checkID != p.socket.keepsEchoID(runtime.GOOS) {
		t.Errorf("%s socket on %s: checkID = %v", p.socket, runtime.GOOS, p.checkID)
	}
	return p
}

func mustAccept(t *testing.T, p *ICMPProber, target string) {
	t.Helper()
	if err := p.Accept(target); err != nil {
		t.Fatal(err)
	}
}

// checkReplies checks that every request was answered, with the details that
// do not depend on the socket type.
func checkReplies(t *testing.T, version ProbeType, body string, events []*Event) {
	t.Helper()
	counts := make(map[reason]int)
	for _, e := range events {
		counts[e.Result]++
		if e.Result != SUCCESS {
			continue
		}
		if e.Details == nil || e.Details.ICMP == nil {
			t.Fatalf("SUCCESS without ICMP details: %+v", e)
		}
		d := e.Details.ICMP
		if d.Payload != body {
			t.Errorf("payload = %q, want %q", d.Payload, body)
		}
		// The ICMP message without the IP header
		if d.PacketSize != 8+len(body) {
			t.Errorf("packet size = %d, want %d", d.PacketSize, 8+len(body))
		}
		if want := map[ProbeType]int{ICMPV4: 0, ICMPV6: 129}[version]; d.ICMPType != want || d.ICMPCode != 0 {
			t.Errorf("ICMP type %d code %d, want an echo reply (%d)", d.ICMPType, d.ICMPCode, want)
		}
		if d.Sequence < 1 {
			t.Errorf("sequence = %d, want > 0", d.Sequence)
		}
		// The real socket must report the reply TTL / hop limit,
		// except on Windows, which has no control messages.
		if runtime.GOOS != "windows" && d.ReplyTTL <= 0 {
			t.Errorf("ReplyTTL = %d, want > 0", d.ReplyTTL)
		}
	}
	if counts[SUCCESS] == 0 || counts[SUCCESS] != counts[SENT] || counts[TIMEOUT]+counts[FAILED] != 0 {
		t.Errorf("not every request was answered: %s", describeEvents(events))
	}
}

func TestIntegrationICMPProberLifecycle(t *testing.T) {
	cases := []struct {
		version ProbeType
		target  string
	}{
		{ICMPV4, "127.0.0.1"},
		{ICMPV6, "::1"},
	}
	for _, tc := range cases {
		t.Run(string(tc.version), func(t *testing.T) {
			base := runtime.NumGoroutine()
			p := integrationProber(t, tc.version, &ICMPConfig{Body: "mping-it"}, string(tc.version))
			mustAccept(t, p, string(tc.version)+"://"+tc.target)

			res := runLifecycle(t, p, []string{tc.target}, 100*time.Millisecond, 500*time.Millisecond)
			checkReplies(t, tc.version, "mping-it", res.events)
			if goroutineRunning("(*ICMPProber).recvPkts") {
				t.Error("receive goroutine still running after Stop")
			}
			stopReturns(t, p) // second Stop
			waitGoroutines(t, base)
		})
	}
}

// TOS, TTL and the source address take effect on either socket type.
func TestIntegrationICMPProberSocketOptions(t *testing.T) {
	cfg := &ICMPConfig{Body: "opts", TOS: 0xb8, TTL: 7, SourceInterface: "127.0.0.1"}
	p := integrationProber(t, ICMPV4, cfg, "custom")

	ic := icmpConnOf(t, p)
	if tos, err := ic.IPv4PacketConn().TOS(); err != nil || tos != cfg.TOS {
		t.Errorf("TOS = %#x, %v; want %#x", tos, err, cfg.TOS)
	}
	if ttl, err := ic.IPv4PacketConn().TTL(); err != nil || ttl != cfg.TTL {
		t.Errorf("TTL = %d, %v; want %d", ttl, err, cfg.TTL)
	}
	if src := peerIPAddr(ic.LocalAddr()); src.String() != cfg.SourceInterface {
		t.Errorf("source address = %v, want %s", src, cfg.SourceInterface)
	}

	mustAccept(t, p, "custom://127.0.0.1")
	res := runLifecycle(t, p, []string{"custom://127.0.0.1"}, 50*time.Millisecond, 500*time.Millisecond)
	checkReplies(t, ICMPV4, "opts", res.events)
}

func icmpConnOf(t *testing.T, p *ICMPProber) *icmpConn {
	t.Helper()
	c := p.c
	if d, ok := c.(datagramConn); ok {
		c = d.packetConn
	}
	ic, ok := c.(*icmpConn)
	if !ok {
		t.Fatalf("prober connection is a %T", p.c)
	}
	return ic
}

// A source address of another host is reported, not papered over by the
// other socket type.
func TestIntegrationICMPProberBadSource(t *testing.T) {
	integrationProber(t, ICMPV4, &ICMPConfig{}, "icmpv4") // skips without a socket
	p, err := NewICMPProber(ICMPV4, &ICMPConfig{SourceInterface: "192.0.2.77"}, "icmpv4")
	if err == nil {
		p.Stop()
		t.Fatal("prober bound to 192.0.2.77")
	}
	if !strings.Contains(err.Error(), "192.0.2.77") || !strings.Contains(err.Error(), "source_interface") {
		t.Errorf("error = %v, want it to name the address and source_interface", err)
	}
}

// linkLocalAddr returns a zoned link-local address of this host, preferring
// the loopback interface (which has one on macOS).
func linkLocalAddr(t *testing.T) string {
	t.Helper()
	ifis, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, ifi := range ifis {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() != nil || !n.IP.IsLinkLocalUnicast() {
				continue
			}
			addr := (&net.IPAddr{IP: n.IP, Zone: ifi.Name}).String()
			if ifi.Flags&net.FlagLoopback != 0 {
				return addr
			}
			if found == "" {
				found = addr
			}
		}
	}
	if found == "" {
		t.Skip("no IPv6 link-local address")
	}
	return found
}

// Link-local addresses work with their zone as a target, bound or not, and
// the zone is part of the key.
func TestIntegrationICMPProberLinkLocal(t *testing.T) {
	addr := linkLocalAddr(t)
	for _, source := range []string{"::", addr} {
		t.Run("from "+source, func(t *testing.T) {
			integrationProber(t, ICMPV6, &ICMPConfig{}, "icmpv6").Stop() // skips without a socket
			s, err := openICMPSocket(runtime.GOOS, ICMPV6, source, listenICMP(ICMPV6, &ICMPConfig{}))
			if err != nil {
				t.Fatal(err)
			}
			p := newICMPProber(ICMPV6, s, []byte("ll"), "icmpv6")
			t.Cleanup(p.Stop)
			mustAccept(t, p, "icmpv6://"+addr)
			res := runLifecycle(t, p, []string{addr}, 50*time.Millisecond, 500*time.Millisecond)
			checkReplies(t, ICMPV6, "ll", res.events)
		})
	}
}

// mutedConn drops everything it is asked to send, like a path that loses
// every request, but still receives.
type mutedConn struct{ packetConn }

func (c mutedConn) WriteTo(b []byte, _ net.Addr) (int, error) { return len(b), nil }

// Probers of the same address, with the same sequence numbers at the same
// time, each get their own replies and nobody else's: whichever of them
// answers is recognised by its body, and the one whose requests are lost
// sees no reply at all.
func TestIntegrationICMPProbersSameAddress(t *testing.T) {
	for version, addr := range map[ProbeType]string{ICMPV4: "127.0.0.1", ICMPV6: "::1"} {
		t.Run(string(version), func(t *testing.T) {
			const n = 4
			var (
				wg      sync.WaitGroup
				probers []*ICMPProber
				logs    []*eventLog
				chans   []chan *Event
			)
			ids := make(map[int]bool)
			for i := range n {
				name := fmt.Sprintf("p%d", i)
				p := integrationProber(t, version, &ICMPConfig{Body: name, TTL: 60 + i, TOS: 4 * i}, name)
				if ids[p.runID] {
					t.Errorf("echo ID %#x used twice", p.runID)
				}
				ids[p.runID] = true
				if i == 0 {
					p.c = muted(p.c)
				}
				mustAccept(t, p, name+"://"+addr)
				ch := make(chan *Event, 1000)
				probers, chans, logs = append(probers, p), append(chans, ch), append(logs, collectEvents(ch))
			}
			for i, p := range probers {
				wg.Go(func() {
					if err := p.Start(chans[i], 20*time.Millisecond, 500*time.Millisecond); err != nil {
						t.Errorf("Start() error = %v", err)
					}
				})
			}
			waitFor(t, "rounds", func() bool {
				for _, l := range logs[1:] {
					if l.count(SENT) < 30 {
						return false
					}
				}
				return logs[0].count(TIMEOUT) > 0
			})
			for _, p := range probers {
				stopReturns(t, p)
			}
			wg.Wait()
			for i, ch := range chans {
				close(ch)
				<-logs[i].done
				events := logs[i].snapshot()
				if i > 0 {
					checkReplies(t, version, fmt.Sprintf("p%d", i), events)
					continue
				}
				if got := logs[i].count(SUCCESS); got != 0 {
					t.Errorf("the prober whose requests are lost counted %d replies", got)
				}
			}
		})
	}
}

// muted mutes c; datagram connections stay that.
func muted(c packetConn) packetConn {
	if d, ok := c.(datagramConn); ok {
		d.packetConn = muted(d.packetConn)
		return d
	}
	return mutedConn{c}
}

// The echo ID of a Linux datagram socket is the one the kernel puts into the
// requests, and it is out of circulation for the raw sockets of the process.
func TestIntegrationICMPProberKernelID(t *testing.T) {
	p := integrationProber(t, ICMPV4, &ICMPConfig{}, "icmpv4")
	if runtime.GOOS != "linux" || p.socket != datagramSocket {
		t.Skip("Linux datagram sockets only")
	}
	id := kernelEchoID(icmpConnOf(t, p))
	if id == 0 || int(id) != p.runID {
		t.Errorf("kernel echo ID %#x, prober has %#x", id, p.runID)
	}
	if l, ok := icmpid.Reserve(id); ok {
		l.Release()
		t.Error("the ID of the open socket is still free")
	}
}
