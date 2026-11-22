//go:build !windows

package prober

import (
	"net"
	"syscall"

	"golang.org/x/net/icmp"
)

// allowBroadcast lets c send to broadcast addresses, such as the last address
// of a subnet given as a target ("192.168.1.0/24"). The net package sets this
// option on the sockets it opens, raw ICMP ones included, but x/net/icmp opens
// the datagram ones itself and does not, so that sending would fail with
// EACCES. Without the option only such targets fail, hence no error.
func allowBroadcast(c *icmp.PacketConn) {
	p := c.IPv4PacketConn()
	if p == nil {
		return
	}
	u, ok := p.PacketConn.(*net.UDPConn)
	if !ok {
		return
	}
	if rc, err := u.SyscallConn(); err == nil {
		_ = rc.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}
}
