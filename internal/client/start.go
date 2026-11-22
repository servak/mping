package client

import (
	"context"
	"errors"
	"time"

	"github.com/servak/mping/internal/api"
)

const maxStarts = 3 // of a server that does not stay

// CreateAutoStart is Create on the default socket, where it starts a server if
// none is running and passes its process ID to started. A server that ends takes
// its watches with it, so a request that it did not answer is sent to the next.
func (c *Client) CreateAutoStart(ctx context.Context, req api.CreateRequest, started func(pid int)) (api.Watch, error) {
	starts := 0
	for deadline := time.Now().Add(endTimeout); ; {
		res, err := c.Create(ctx, req)
		var e *api.Error
		switch {
		case err != nil && lost(err) && starts < maxStarts:
			starts++
			pid, err := c.startServer(ctx)
			if err != nil {
				return res, err
			}
			if pid != 0 {
				started(pid)
			}
		case errors.As(err, &e) && e.Code == api.CodeShuttingDown && time.Now().Before(deadline):
			if err := sleep(ctx, pollInterval); err != nil {
				return res, err
			}
		default:
			return res, err
		}
	}
}
