//go:build linux

package trace

import (
	"net/netip"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// enableRecvErr makes the kernel queue ICMP errors for datagrams sent from
// conn (UDP or datagram ICMP) on its error queue (IP_RECVERR /
// IPV6_RECVERR). This needs no privileges and also works for unconnected
// sockets.
func enableRecvErr(conn syscall.Conn, v6 bool) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		if v6 {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVERR, 1)
		} else {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVERR, 1)
		}
	})
	if err != nil {
		return err
	}
	if serr != nil {
		return os.NewSyscallError("setsockopt", serr)
	}
	return nil
}

// receiveQueues delivers the ICMP errors on the error queue of conn and the
// datagrams it receives until it is closed. A queued error makes the socket
// readable (EPOLLERR), so the runtime poller wakes us up like for normal data.
func receiveQueues(conn syscall.Conn, onErr func(queuedError), onData func(b []byte, from string, at time.Time)) {
	rc, err := conn.SyscallConn()
	if err != nil {
		return
	}
	buf := make([]byte, maxPacketSize)
	oob := make([]byte, 512)
	var poller queuePoller
	for {
		var queue msgQueue
		var n, oobn int
		var from unix.Sockaddr
		err := rc.Read(func(fd uintptr) bool {
			var wait bool
			queue, wait = poller.next(
				func() (err error) {
					n, oobn, _, from, err = unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
					return err
				},
				func() (err error) {
					n, from, err = unix.Recvfrom(int(fd), buf, unix.MSG_DONTWAIT)
					return err
				})
			return !wait
		})
		if err != nil {
			return // closed
		}
		now := time.Now()
		addr := sockaddrAddrPort(from)
		switch queue {
		case dataQueue:
			onData(buf[:n], addr.Addr().String(), now)
		case errQueue:
			msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
			if err != nil {
				continue
			}
			for _, m := range msgs {
				if (m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_RECVERR) ||
					(m.Header.Level == unix.IPPROTO_IPV6 && m.Header.Type == unix.IPV6_RECVERR) {
					onErr(queuedError{payload: buf[:n], dst: addr, data: m.Data, at: now})
				}
			}
		}
	}
}

// sockaddrAddrPort converts sa; the result is invalid if sa is not an IP
// address. The IPv6 zone is dropped, as peerIP does.
func sockaddrAddrPort(sa unix.Sockaddr) netip.AddrPort {
	switch sa := sa.(type) {
	case *unix.SockaddrInet4:
		return netip.AddrPortFrom(netip.AddrFrom4(sa.Addr), uint16(sa.Port))
	case *unix.SockaddrInet6:
		return netip.AddrPortFrom(netip.AddrFrom16(sa.Addr), uint16(sa.Port))
	}
	return netip.AddrPort{}
}
