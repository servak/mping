//go:build linux

package trace

import "syscall"

// platformICMPErrnos are the rest of the errnos that the kernel translates
// ICMP errors to (icmp_err_convert, icmpv6_err_convert, ping_err): protocol
// unreachable, fragmentation needed, source route failed, host unknown, host
// isolated, administratively prohibited, redirect / source quench. Some of
// them are errors of a send as well; repeating that send does no harm.
var platformICMPErrnos = []syscall.Errno{
	syscall.ENOPROTOOPT, syscall.EMSGSIZE, syscall.EOPNOTSUPP, syscall.EHOSTDOWN,
	syscall.ENONET, syscall.EACCES, syscall.EREMOTEIO,
}

var platformSendErrnos []syscall.Errno
