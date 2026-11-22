// Package icmpsock holds what the ICMP probers (internal/prober) and the path
// tracers (internal/trace) share about opening ICMP sockets: datagram
// ("ping") sockets, which need no privileges where they are permitted, and
// raw ones, which need root or CAP_NET_RAW (Administrator on Windows).
package icmpsock

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"syscall"

	"github.com/servak/mping/internal/icmpid"
)

// DatagramSupported reports whether golang.org/x/net/icmp supports datagram
// ICMP on goos. It needs no privileges, but on Linux the group must be within
// net.ipv4.ping_group_range (which governs IPv6 too).
func DatagramSupported(goos string) bool {
	return goos == "linux" || goos == "darwin"
}

// DatagramKeepsEchoID reports whether replies on a datagram socket carry the
// echo ID we put in the request. Linux replaces the ID with one of its own
// per socket and delivers to a socket only the replies to that ID, so there
// the ID must not, and need not, be checked. macOS delivers the replies to
// every socket on the host, like a raw socket does.
func DatagramKeepsEchoID(goos string) bool { return goos != "linux" }

// MaxReserveAttempts limits how many sockets OpenDatagram opens to get a
// free echo ID.
const MaxReserveAttempts = 3

// OpenDatagram opens a datagram ICMP socket with listen and takes its echo
// ID from ids. The echo ID Linux chose is known if the socket has a LocalAddr
// method, see KernelEchoID.
//
// On Linux that is the ID the kernel chose, reserved so that no raw socket of
// this process gets the same one: a prober or tracer on a raw socket would
// see the replies. Should a raw socket hold the ID already (the kernel does
// not know about those), another socket gets another ID.
func OpenDatagram[C io.Closer](goos string, ids *icmpid.Allocator, listen func() (C, error)) (C, *icmpid.Lease, error) {
	var none C
	for attempt := 1; ; attempt++ {
		c, err := listen()
		if err != nil {
			return none, nil, err
		}
		if id := KernelEchoID(c); !DatagramKeepsEchoID(goos) && id != 0 {
			if lease, ok := ids.Reserve(id); ok {
				return c, lease, nil
			}
			if attempt < MaxReserveAttempts {
				_ = c.Close()
				continue
			}
		}
		lease, err := AcquireID(c, ids)
		if err != nil {
			return none, nil, err
		}
		return c, lease, nil
	}
}

// AcquireID takes a free echo ID for the socket c; without one it closes c.
func AcquireID(c io.Closer, ids *icmpid.Allocator) (*icmpid.Lease, error) {
	lease, err := ids.Acquire()
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("cannot allocate ICMP echo ID: %w", err)
	}
	return lease, nil
}

// KernelEchoID returns the echo ID Linux gave a datagram socket, which is
// its local port, or 0 when unknown.
func KernelEchoID(c any) uint16 {
	if l, ok := c.(interface{ LocalAddr() net.Addr }); ok {
		if a, ok := l.LocalAddr().(*net.UDPAddr); ok && a != nil {
			return uint16(a.Port)
		}
	}
	return 0
}

// Denied reports whether err is a lack of privileges. Winsock's WSAEACCES
// is not the syscall.EACCES that fs.ErrPermission stands for.
func Denied(err error) bool {
	const wsaEACCES = syscall.Errno(10013)
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, wsaEACCES)
}

// DatagramUnavailable reports whether err means that this process may not
// use datagram ICMP (EACCES outside of ping_group_range, EPERM from a
// sandbox) or that the kernel does not have it. Any other failure (a source
// address this host does not have, no IPv6, no file descriptors) is one a
// raw socket would share.
func DatagramUnavailable(err error) bool {
	return Denied(err) || errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.ESOCKTNOSUPPORT)
}

// PrivilegeHint says how to get permission to open an ICMP socket.
func PrivilegeHint(goos string) string {
	switch goos {
	case "linux":
		return ` (allow datagram ICMP for your group with: sudo sysctl -w net.ipv4.ping_group_range="0 2147483647";` +
			` or allow mping to use raw sockets with: sudo setcap cap_net_raw=+ep /path/to/mping)`
	case "windows":
		return " (run as Administrator)"
	}
	return " (run as root)"
}
