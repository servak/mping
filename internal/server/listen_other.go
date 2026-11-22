//go:build !darwin && !linux && !freebsd

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
)

// Locked tells whether a process holds the lock of a socket.
func Locked(string) bool { return false }

func listen(context.Context, string) (net.Listener, func(), error) {
	return nil, nil, fmt.Errorf("mping server on %s: %w", runtime.GOOS, errors.ErrUnsupported)
}
