//go:build !linux

package trace

import (
	"net"
	"time"
)

// enableRecvErr is Linux only: other platforms have no socket error queue
func enableRecvErr(*net.UDPConn, bool) error { return errUDPUnsupported }

func (t *udpTransport) receiveErrQueue(func(reply, string, time.Time)) {}
