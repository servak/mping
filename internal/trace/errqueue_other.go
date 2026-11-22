//go:build !linux

package trace

import (
	"syscall"
	"time"
)

// enableRecvErr is Linux only: other platforms have no socket error queue
func enableRecvErr(syscall.Conn, bool) error { return errUDPUnsupported }

func receiveQueues(syscall.Conn, func(queuedError), func([]byte, string, time.Time)) {}
