//go:build !darwin && !linux && !freebsd

package client

import (
	"context"
	"errors"
)

func (c *Client) startServer(context.Context) (pid int, err error) {
	return 0, errors.ErrUnsupported
}
