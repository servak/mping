//go:build windows

package trace

import "syscall"

// Winsock reports WSA error codes, which the syscall constants (invented
// values on Windows) do not match.
var (
	platformICMPErrnos = []syscall.Errno{
		10054, // WSAECONNRESET: an earlier probe got Port Unreachable
		10065, // WSAEHOSTUNREACH
		10051, // WSAENETUNREACH
	}
	platformSendErrnos = []syscall.Errno{
		10050, // WSAENETDOWN
		10064, // WSAEHOSTDOWN
		10055, // WSAENOBUFS
		10035, // WSAEWOULDBLOCK
		10013, // WSAEACCES
		10040, // WSAEMSGSIZE
		10049, // WSAEADDRNOTAVAIL
	}
)
