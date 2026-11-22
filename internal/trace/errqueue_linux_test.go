//go:build linux

package trace

import (
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

// Every errno that the kernel translates an ICMP error to (icmp_err_convert,
// icmpv6_err_convert, ping_err) may be pending on a socket with IP_RECVERR.
// It must neither lose the next probe nor end the trace.
func TestLinuxICMPErrnos(t *testing.T) {
	for _, errno := range []syscall.Errno{
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENOPROTOOPT, syscall.ECONNREFUSED,
		syscall.EMSGSIZE, syscall.EOPNOTSUPP, syscall.EHOSTDOWN, syscall.ENONET, // ICMP
		syscall.EACCES, syscall.EPROTO, // ICMPv6
		syscall.EREMOTEIO, // redirect, source quench
	} {
		if !isICMPErrno(sendErrno(errno)) || !isTransientSendErr(sendErrno(errno)) {
			t.Errorf("%v is not taken for an ICMP error", errno)
		}
	}
}

// The error queue and the receive queue of a real socket (UDP, which needs no
// permission): the errors of closed ports, with their pending socket errors,
// and datagrams that arrive in between are all delivered.
func TestReceiveQueues(t *testing.T) {
	for _, tt := range []struct {
		network string
		ip      net.IP
		v6      bool
	}{{"udp4", net.IPv4(127, 0, 0, 1), false}, {"udp6", net.IPv6loopback, true}} {
		t.Run(tt.network, func(t *testing.T) {
			listen := func() *net.UDPConn {
				c, err := net.ListenUDP(tt.network, &net.UDPAddr{IP: tt.ip})
				if err != nil {
					t.Skipf("no %s: %v", tt.network, err)
				}
				return c
			}
			peer, conn, closedPort := listen(), listen(), listen()
			defer peer.Close()
			defer conn.Close()
			dead := closedPort.LocalAddr().(*net.UDPAddr)
			closedPort.Close()
			if err := enableRecvErr(conn, tt.v6); err != nil {
				t.Fatal(err)
			}
			errs, data := make(chan queuedError, 64), make(chan string, 64)
			done := make(chan struct{})
			go func() {
				defer close(done)
				receiveQueues(conn, func(e queuedError) {
					e.payload, e.data = append([]byte(nil), e.payload...), append([]byte(nil), e.data...)
					errs <- e
				}, func(b []byte, from string, at time.Time) {
					data <- fmt.Sprintf("%s from %s", b, from)
				})
			}()

			const n = 5 // below the kernel's burst of ICMP errors (6)
			for i := range n {
				payload := udpProbePayload(i + 1)
				if err := retrySend(func() error { _, err := conn.WriteToUDP(payload, dead); return err }); err != nil {
					t.Errorf("send %d: %v", i, err)
				}
				if _, err := peer.WriteToUDP([]byte{'a' + byte(i)}, conn.LocalAddr().(*net.UDPAddr)); err != nil {
					t.Fatal(err)
				}
			}
			seen := 0
			for i := range n {
				select {
				case got := <-data:
					if want := fmt.Sprintf("%c from %s", 'a'+i, tt.ip); got != want {
						t.Errorf("datagram %q, want %q", got, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("datagram %d not delivered", i)
				}
			}
			for seen < n {
				select {
				case e := <-errs:
					seen++
					r, from, err := parseExtendedErr(e.data, e.dst.Port())
					if err != nil || r.kind != kindUnreachable || int(r.seq) != dead.Port || from != tt.ip.String() {
						t.Errorf("error %d: %+v from %s, %v", seen, r, from, err)
					}
					if ttl := errQueuePayloadTTL(e.payload); ttl < 1 || ttl > n {
						t.Errorf("error %d quotes %q", seen, e.payload)
					}
				case <-time.After(2 * time.Second):
					// The kernel limits the rate of ICMP errors
					if seen == 0 {
						t.Fatal("no ICMP error delivered")
					}
					t.Logf("%d of %d ICMP errors", seen, n)
					seen = n
				}
			}

			conn.Close()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("still receiving after close")
			}
		})
	}
}
