//go:build darwin || linux || freebsd

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/server"
)

const (
	idleExit     = "10m"
	startTimeout = 10 * time.Second
	maxLog       = 1 << 20
	logTail      = 10 // lines of the log in an error
)

// startServer makes sure that a server runs on the socket, which must be the
// default one, and returns the process ID of the new server or 0 if another one
// was there first. Any number of processes may do so at once: the lock of the
// server lets one of their servers live.
func (c *Client) startServer(ctx context.Context) (pid int, err error) {
	var child *process
	for deadline := time.Now().Add(endTimeout); ; {
		ended := child != nil && child.ended() // before the others, who may have been faster
		h, err := c.Health(ctx)
		switch {
		case err == nil && child != nil && h.PID == child.pid:
			return child.pid, nil
		case err == nil:
			return 0, nil
		case !lost(err):
			return 0, err
		case time.Now().After(deadline):
			err := errors.New("the server does not listen, but has not ended either")
			if child != nil {
				err = fmt.Errorf("the server (pid %d) does not listen%s", child.pid, child.output())
			}
			return 0, err
		case server.Locked(c.Socket): // it is starting or ending
		case ended:
			return 0, fmt.Errorf("the server did not start%s", child.output())
		case child == nil:
			if child, err = spawn(filepath.Join(filepath.Dir(c.Socket), "server.log")); err != nil {
				return 0, fmt.Errorf("starting the server: %w", err)
			}
			deadline = time.Now().Add(startTimeout)
		}
		if err := sleep(ctx, pollInterval); err != nil {
			return 0, err
		}
	}
}

// process is a server that has just been started.
type process struct {
	pid    int
	done   chan struct{} // closed when it has ended
	log    string        // path
	offset int64         // where its output begins
}

// spawn starts a server that shares nothing with this process and its terminal.
func spawn(logPath string) (*process, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(logPath)
	if home, err := os.Stat(filepath.Dir(dir)); err == nil && os.Getuid() == 0 {
		// What root leaves there would be in the way of its owner
		if st, ok := home.Sys().(*syscall.Stat_t); ok && st.Uid != 0 {
			return nil, fmt.Errorf("the home directory %s is not that of root; with sudo, use -H", filepath.Dir(dir))
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	info, err := log.Stat()
	if err != nil {
		return nil, err
	}
	offset := info.Size()
	if offset > maxLog {
		if err := log.Truncate(0); err != nil {
			return nil, err
		}
		offset = 0
	}
	// Those that this process got from its parent, who may wait for their end
	for fd := 3; fd < 1024; fd++ {
		syscall.CloseOnExec(fd)
	}
	cmd := exec.Command(exe, "server", "--idle-exit", idleExit)
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{pid: cmd.Process.Pid, done: make(chan struct{}), log: logPath, offset: offset}
	go func() {
		defer close(p.done)
		cmd.Wait()
	}()
	return p, nil
}

func (p *process) ended() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// output returns the last lines of the log, to be appended to an error.
func (p *process) output() string {
	s := fmt.Sprintf(" (see %s)", p.log)
	f, err := os.Open(p.log)
	if err != nil {
		return s
	}
	defer f.Close()
	b, _ := io.ReadAll(io.NewSectionReader(f, p.offset, maxLog))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for _, line := range lines[max(0, len(lines)-logTail):] {
		s += "\n  " + api.Clean(line, 500)
	}
	return s
}
