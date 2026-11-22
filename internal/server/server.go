// Package server is the mping server: an HTTP API for watches on a Unix socket
// that only its user can open.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/watch"
)

// ErrRunning is the error of a Run for a socket that another server serves.
var ErrRunning = errors.New("a server is already running")

// shutdownTimeout bounds the end of a server, for which its watches wait for
// their last replies.
var shutdownTimeout = watch.DefaultLimits.MaxTimeout + 5*time.Second

// Options describes a server.
type Options struct {
	Socket     string                         // path; "" for DefaultSocket
	LoadConfig func() (*config.Config, error) // called for each new watch
	Version    string
	Logger     *slog.Logger // required
	// IdleExit ends the server when there has been no watch at all, not even a
	// stopped one, for that long. 0: never.
	IdleExit time.Duration
}

// DefaultSocket returns the path of the socket of the user's server.
func DefaultSocket() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && !filepath.IsAbs(home) {
		err = fmt.Errorf("the home directory is not an absolute path: %q", home)
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mping", "server.sock"), nil
}

// Run serves until ctx is done, a client asks for the end or it is idle for
// too long (nil for all of these), or it fails. It returns when all watches
// have stopped; those that take too long are an error.
func Run(ctx context.Context, o Options) error {
	if o.Socket == "" {
		var err error
		if o.Socket, err = DefaultSocket(); err != nil {
			return err
		}
	}
	ln, unlock, err := listen(ctx, o.Socket)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watches := watch.NewManager(o.LoadConfig)
	srv := &http.Server{
		Handler:           newHandler(watches, o.Version, o.Logger, cancel),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      time.Minute, // creating a watch may take half of it
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(o.Logger.Handler(), slog.LevelWarn),

		DisableGeneralOptionsHandler: true,
	}
	o.Logger.Info("listening", "socket", o.Socket, "version", o.Version)
	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		cancel()
	}()
	if o.IdleExit > 0 {
		go func() {
			ticker := time.NewTicker(max(o.IdleExit/20, time.Millisecond))
			defer ticker.Stop()
			for !watches.CloseIdle(o.IdleExit) {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
			cancel()
		}()
	}
	<-ctx.Done()

	o.Logger.Info("shutting down")
	ctx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	stopped := make(chan error, 1)
	go func() { stopped <- watches.Shutdown(ctx) }() // also ends requests that create a watch
	err = srv.Shutdown(ctx)
	if err != nil {
		srv.Close()
	}
	err = errors.Join(err, <-stopped)
	select {
	case failed := <-serveErr:
		err = errors.Join(failed, err)
	default:
	}
	return err
}
