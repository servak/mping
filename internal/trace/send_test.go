package trace

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func sendErrno(e syscall.Errno) error {
	return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", e)}
}

// A transient send error (no route, firewall, full buffers) loses only that
// probe; the trace goes on.
func TestTransientSendErrorKeepsTracing(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENOBUFS, syscall.EPERM} {
		fake := newFakeNetwork(1)
		var mu sync.Mutex
		calls := 0
		fake.sendErr = func(ttl int) error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls <= 2 { // the first two probes of round 1
				return sendErrno(errno)
			}
			return nil
		}
		tr := newTracer(Config{Interval: 5 * time.Millisecond, Timeout: 20 * time.Millisecond, NoDNS: true, Proto: ProtoUDP},
			[]byte{192, 0, 2, 10}, fake)
		if err := tr.Run(context.Background(), 4); err != nil {
			t.Fatalf("%v: Run() error = %v", errno, err)
		}
		res := tr.Snapshot()
		if res.Rounds != 4 || !res.Reached || len(res.Hops) != 3 {
			t.Fatalf("%v: result = %+v", errno, res)
		}
		if h := res.Hops[0]; h.Sent != 4 || h.Recv != 3 {
			t.Errorf("%v: hop 1 sent=%d recv=%d, want the failed probe lost (4/3)", errno, h.Sent, h.Recv)
		}
	}
}

func TestFatalSendErrorEndsTrace(t *testing.T) {
	fake := newFakeNetwork(1)
	fake.sendErr = func(int) error { return sendErrno(syscall.EBADF) }
	tr := newTracer(Config{Interval: 5 * time.Millisecond, Timeout: 20 * time.Millisecond, NoDNS: true, Proto: ProtoUDP},
		[]byte{192, 0, 2, 10}, fake)
	if err := tr.Run(context.Background(), 4); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("Run() error = %v, want EBADF", err)
	}
}
