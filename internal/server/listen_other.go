//go:build !darwin && !linux && !freebsd

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
)

func listen(context.Context, string) (net.Listener, func(), error) {
	return nil, nil, fmt.Errorf("mping server on %s: %w", runtime.GOOS, errors.ErrUnsupported)
}
