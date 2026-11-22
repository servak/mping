//go:build integration && !windows

package prober

import (
	"syscall"
	"testing"
)

// Either socket type may send to broadcast addresses, see allowBroadcast.
func TestIntegrationICMPProberBroadcastAllowed(t *testing.T) {
	p := integrationProber(t, ICMPV4, &ICMPConfig{}, "icmpv4")
	conn := icmpConnOf(t, p).IPv4PacketConn().PacketConn
	sc, ok := conn.(syscall.Conn)
	if !ok {
		t.Fatalf("no SyscallConn on %T", conn)
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var on int
	if cerr := rc.Control(func(fd uintptr) {
		on, err = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST)
	}); cerr != nil || err != nil || on == 0 {
		t.Errorf("SO_BROADCAST = %d (%v, %v), want it set", on, cerr, err)
	}
}
