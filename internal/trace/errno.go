package trace

import (
	"errors"
	"syscall"
)

// icmpErrnos are the errnos a socket reports for an ICMP error it received
// (Port / Host / Net Unreachable, Time Exceeded): Linux with IP_RECVERR
// returns them from the next socket call, see udpTransport.send.
var icmpErrnos = append([]syscall.Errno{
	syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EPROTO,
}, platformICMPErrnos...)

// localSendErrnos are the errnos of a send that failed for a local reason
// that may go away: no route right now, a firewall rule, full buffers.
var localSendErrnos = append([]syscall.Errno{
	syscall.ENETDOWN, syscall.EHOSTDOWN, syscall.ENOBUFS, syscall.ENOMEM, syscall.EAGAIN,
	syscall.EPERM, syscall.EACCES, syscall.EMSGSIZE, syscall.EADDRNOTAVAIL,
}, platformSendErrnos...)

func isErrno(err error, errnos []syscall.Errno) bool {
	for _, e := range errnos {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// isICMPErrno reports whether err is the errno for a received ICMP error
func isICMPErrno(err error) bool { return isErrno(err, icmpErrnos) }

// isTransientSendErr reports whether a failed send only loses that probe: a
// local routing problem, a firewall rule, full buffers or an ICMP error of
// an earlier probe. Anything else (a broken socket) ends the trace.
func isTransientSendErr(err error) bool {
	return isICMPErrno(err) || isErrno(err, localSendErrnos)
}
