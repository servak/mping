//go:build !windows

package trace

import "syscall"

// The syscall constants are the errnos the kernel returns
var platformICMPErrnos, platformSendErrnos []syscall.Errno
