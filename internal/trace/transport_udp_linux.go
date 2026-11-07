//go:build linux

package trace

import (
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// enableRecvErr makes the kernel queue ICMP errors for datagrams sent from
// conn on its error queue (IP_RECVERR / IPV6_RECVERR). This needs no
// privileges and also works for unconnected sockets.
func enableRecvErr(conn *net.UDPConn, v6 bool) error {
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

// receiveErrQueue reads ICMP errors from the error queue of the UDP socket
// until it is closed. A queued error makes the socket readable (EPOLLERR),
// so the runtime poller wakes us up like for normal data.
func (t *udpTransport) receiveErrQueue(deliver func(reply, string, time.Time)) {
	rc, err := t.conn.SyscallConn()
	if err != nil {
		return
	}
	buf := make([]byte, maxPacketSize)
	oob := make([]byte, 512)
	for {
		var oobn int
		var from unix.Sockaddr
		var rerr error
		err := rc.Read(func(fd uintptr) bool {
			_, oobn, _, from, rerr = unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
			if rerr == unix.EAGAIN || rerr == unix.EWOULDBLOCK {
				// Nothing queued: discard any normal data (a destination
				// actually listening on the port), which would otherwise
				// keep the socket readable, then wait.
				for {
					if _, _, err := unix.Recvfrom(int(fd), buf, unix.MSG_DONTWAIT); err != nil {
						break
					}
				}
				return false
			}
			return true
		})
		if err != nil {
			return // closed
		}
		if rerr != nil {
			continue // EINTR and friends
		}
		now := time.Now()
		var dstPort uint16
		switch sa := from.(type) {
		case *unix.SockaddrInet4:
			dstPort = uint16(sa.Port)
		case *unix.SockaddrInet6:
			dstPort = uint16(sa.Port)
		default:
			continue
		}
		msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			continue
		}
		for _, m := range msgs {
			isErr := (m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_RECVERR) ||
				(m.Header.Level == unix.IPPROTO_IPV6 && m.Header.Type == unix.IPV6_RECVERR)
			if !isErr {
				continue
			}
			r, addr, err := parseExtendedErr(m.Data, dstPort)
			if err != nil {
				continue
			}
			r.id = t.srcPort
			deliver(r, addr, now)
		}
	}
}
