//go:build darwin || linux || freebsd

package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockWait is how long to wait for a server that is ending.
const lockWait = 5 * time.Second

// listen listens on a Unix socket that only this user can open. It holds the
// lock file next to the socket until unlock is called, so that one server at
// most uses the socket; the kernel releases the lock of a crashed one. Closing
// the listener removes the socket.
func listen(ctx context.Context, path string) (ln net.Listener, unlock func(), err error) {
	if path, err = filepath.Abs(path); err != nil {
		return nil, nil, err
	}
	if limit := len(syscall.RawSockaddrUnix{}.Path) - 1; len(path) > limit {
		return nil, nil, fmt.Errorf("socket path is longer than %d bytes: %s", limit, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	lock, err := acquire(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if err := removeStale(path); err != nil {
		lock.Close()
		return nil, nil, err
	}
	// The umask is that of the process, which has only just started
	old := syscall.Umask(0o177)
	ln, err = net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	return ln, func() { lock.Close() }, nil
}

// acquire locks the lock file of a socket. It is never removed: that would let
// two processes lock different files.
func acquire(ctx context.Context, socket string) (*os.File, error) {
	f, err := os.OpenFile(socket+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	for deadline := time.Now().Add(lockWait); ; {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			switch {
			case dial(socket) == nil:
				err = ErrRunning
			case time.Now().After(deadline):
				err = errors.New("another server holds the lock, but does not listen")
			default: // it is starting or ending
				select {
				case <-time.After(50 * time.Millisecond):
					continue
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
		}
		f.Close()
		return nil, fmt.Errorf("%s: %w", socket, err)
	}
}

// Locked tells whether a process holds the lock of a socket: a server, also one
// that is starting or ending and does not listen. The probe may make a server
// that starts at the same moment try again, not fail.
func Locked(socket string) bool {
	f, err := os.OpenFile(socket+".lock", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil
}

// removeStale removes the socket of a server that has crashed. The lock must be
// held.
func removeStale(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode().Type() != fs.ModeSocket:
		return fmt.Errorf("%s exists and is not a socket", path)
	}
	switch err := dial(path); {
	case err == nil:
		return fmt.Errorf("%s: %w", path, ErrRunning)
	case !errors.Is(err, syscall.ECONNREFUSED):
		return err
	}
	return os.Remove(path)
}

// dial returns nil if something listens on the socket.
func dial(path string) error {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		c.Close()
	}
	return err
}
