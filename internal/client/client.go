// Package client talks to the mping server on a Unix socket.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/server"
)

// ErrNotRunning is the error of a request when no server listens on the socket.
var ErrNotRunning = errors.New("the server is not running")

// Variables for tests.
var (
	timeout       = 10 * time.Second
	createTimeout = 45 * time.Second // the server looks up targets for 30s
	// endTimeout is longer than a server takes to end.
	endTimeout   = 70 * time.Second
	pollInterval = 50 * time.Millisecond
)

const maxResponse = 64 << 20

// Client is a client of the server on a socket, for one goroutine.
type Client struct {
	Socket string
	// ServerVersion is that of the server that answered last.
	ServerVersion string

	http *http.Client
}

// New returns a Client. It does not connect.
func New(socket string) *Client {
	return &Client{Socket: socket, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := new(net.Dialer).DialContext(ctx, "unix", socket)
			if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
				err = fmt.Errorf("%w: nothing listens on %s", ErrNotRunning, socket)
			} else if errors.Is(err, syscall.EINVAL) {
				err = fmt.Errorf("%w (is the path too long?)", err)
			}
			return conn, err
		},
	}}}
}

// Health returns the state of the server.
func (c *Client) Health(ctx context.Context) (api.Health, error) {
	return request[api.Health](ctx, c, http.MethodGet, "/healthz", nil)
}

// Create starts a watch.
func (c *Client) Create(ctx context.Context, req api.CreateRequest) (api.Watch, error) {
	return request[api.Watch](ctx, c, http.MethodPost, "/v1/watches", req)
}

// List returns all watches, oldest first.
func (c *Client) List(ctx context.Context) (api.WatchList, error) {
	return request[api.WatchList](ctx, c, http.MethodGet, "/v1/watches", nil)
}

// Get returns a watch by ID or name, as do the following ones.
func (c *Client) Get(ctx context.Context, id string) (api.WatchDetail, error) {
	return request[api.WatchDetail](ctx, c, http.MethodGet, watchPath(id), nil)
}

// Summary returns the state of a watch, with all targets or those not ok.
func (c *Client) Summary(ctx context.Context, id string, all bool) (api.Summary, error) {
	path := watchPath(id) + "/summary"
	if all {
		path += "?targets=all"
	}
	return request[api.Summary](ctx, c, http.MethodGet, path, nil)
}

// Events returns the events of a watch after a sequence number, or as many as
// the server sends at once.
func (c *Client) Events(ctx context.Context, id string, after int64) (api.Events, error) {
	return request[api.Events](ctx, c, http.MethodGet, watchPath(id)+"/events?after="+strconv.FormatInt(after, 10), nil)
}

// Extend makes a watch end in that many seconds from now.
func (c *Client) Extend(ctx context.Context, id string, seconds int64) (api.Watch, error) {
	return request[api.Watch](ctx, c, http.MethodPost, watchPath(id)+"/extend", api.ExtendRequest{DurationS: seconds})
}

// Stop begins to stop a watch.
func (c *Client) Stop(ctx context.Context, id string) (api.Watch, error) {
	return request[api.Watch](ctx, c, http.MethodPost, watchPath(id)+"/stop", nil)
}

// Delete removes a watch, with force also one that has not stopped.
func (c *Client) Delete(ctx context.Context, id string, force bool) error {
	path := watchPath(id)
	if force {
		path += "?force=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// Shutdown ends the server, with force also while watches are running, and
// waits until it has released the socket.
func (c *Client) Shutdown(ctx context.Context, force bool) error {
	before, err := c.Health(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, http.MethodPost, "/v1/shutdown", api.ShutdownRequest{Force: force}, nil)
	if err != nil && !lost(err) { // it may be ending already
		return err
	}
	for deadline := time.Now().Add(endTimeout); ; {
		h, err := c.Health(ctx)
		switch {
		case errors.Is(err, ErrNotRunning) && !server.Locked(c.Socket):
			return nil
		case err == nil && h.PID != before.PID: // the next one already
			return nil
		case time.Now().After(deadline):
			return fmt.Errorf("the server (pid %d) did not end within %v", before.PID, endTimeout)
		}
		if err := sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
}

func watchPath(id string) string { return "/v1/watches/" + url.PathEscape(id) }

// lost tells whether err is that of a server that is not there or went away
// before it answered.
func lost(err error) bool {
	return errors.Is(err, ErrNotRunning) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// request returns the answer to a request.
func request[T any](ctx context.Context, c *Client, method, path string, body any) (res T, err error) {
	err = c.do(ctx, method, path, body, &res)
	return res, err
}

// do sends a request, with body as JSON unless it is nil, and decodes the
// answer into res unless that is nil. An error of the server is an *api.Error.
func (c *Client) do(ctx context.Context, method, path string, body, res any) error {
	limit := timeout
	if method == http.MethodPost && path == "/v1/watches" {
		limit = createTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, "http://mping"+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	status, b, err := c.roundTrip(req)
	switch {
	case err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("the server did not answer within %v", limit)
	case err != nil:
		return err
	case status/100 != 2:
		var e api.ErrorBody
		if json.Unmarshal(b, &e) != nil || e.Error == nil {
			return fmt.Errorf("unexpected answer from %s: status %d", c.Socket, status)
		}
		return e.Error
	case res == nil:
		return nil
	}
	if err := json.Unmarshal(b, res); err != nil {
		return fmt.Errorf("unexpected answer from %s: %w", c.Socket, err)
	}
	return nil
}

func (c *Client) roundTrip(req *http.Request) (status int, body []byte, err error) {
	resp, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error // its URL is not a real one
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		if lost(err) && !errors.Is(err, ErrNotRunning) {
			err = fmt.Errorf("the server went away: %w", err)
		}
		return 0, nil, err
	}
	defer resp.Body.Close()
	c.ServerVersion = resp.Header.Get(api.VersionHeader)
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err == nil && len(body) > maxResponse {
		err = fmt.Errorf("the answer of %s is larger than %d bytes", c.Socket, maxResponse)
	}
	return resp.StatusCode, body, err
}
