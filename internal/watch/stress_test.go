package watch

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/config"
)

// tcpTarget returns a target on loopback that answers.
func tcpTarget(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return "tcp://" + ln.Addr().String()
}

func realManager(t *testing.T) *Manager {
	limits := DefaultLimits
	limits.MinInterval = 5 * time.Millisecond
	limits.Running = 8
	m := NewManager(func() (*config.Config, error) { return config.DefaultConfig(), nil })
	m.limits = limits
	return m
}

func shutdown(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}

// All operations at the same time, with real sessions.
func TestConcurrentUse(t *testing.T) {
	m := realManager(t)
	// Nothing listens on the second one
	targets := []string{tcpTarget(t), "tcp://127.0.0.1:1"}
	stop := time.After(2 * time.Second)
	done := make(chan struct{})
	go func() { <-stop; close(done) }()
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			name := fmt.Sprint("w", i%10)
			for {
				select {
				case <-done:
					return
				default:
				}
				var err error
				switch rand.IntN(10) {
				case 0, 1, 2:
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(rand.IntN(50))*time.Millisecond)
					_, err = m.Create(ctx, api.CreateRequest{Name: name, Targets: targets, IntervalMs: 5, TimeoutMs: 20})
					cancel()
				case 3:
					_, err = m.Stop(name)
				case 4:
					err = m.Delete(name, rand.IntN(2) == 0)
				case 5:
					_, err = m.Extend(name, 7200)
				case 6:
					m.List()
					m.tick()
				case 7:
					_, err = m.Get(name)
				case 8:
					_, err = m.Summary(name, true)
				case 9:
					_, err = m.Events(name, 0, 100)
				}
				var ae *api.Error
				if err != nil && (!errors.As(err, &ae) || ae.Code == api.CodeInternal && ae.Message != "canceled") {
					t.Errorf("%#v", err)
				}
				if n := m.activeCount(); n > 8 {
					t.Errorf("%d watches are running", n)
				}
			}
		})
	}
	workers.Wait()
	shutdown(t, m)
	for _, w := range m.List() {
		if w.State != api.StateStopped {
			t.Errorf("%+v", w)
		}
		events, err := m.Events(w.ID, 0, 100000)
		if err != nil {
			t.Error(err)
			continue
		}
		n, down := len(events.Events), make(map[string]bool)
		for i, e := range events.Events {
			switch e.Kind {
			case "outage_start", "outage_end":
				start := e.Kind == "outage_start"
				if down[e.Target] == start {
					t.Errorf("%s: %s out of order", w.ID, e.Kind)
				}
				down[e.Target] = start
			}
			if (e.Kind == api.EventWatchStarted) != (i == 0) || (e.Kind == api.EventWatchStopped) != (i == n-1) {
				t.Errorf("%s: event %d of %d is %s", w.ID, i, n, e.Kind)
			}
		}
		for target, d := range down {
			if d {
				t.Errorf("%s: the outage of %s is not closed", w.ID, target)
			}
		}
	}
}

func (m *Manager) activeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active()
}

// fds returns the number of open files of the process.
func fds(t *testing.T) int {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip(err)
	}
	return len(entries)
}

func TestNoLeaks(t *testing.T) {
	target := tcpTarget(t)
	cycle := func(m *Manager) {
		w, err := m.Create(context.Background(), api.CreateRequest{Name: "w", Targets: []string{target}, IntervalMs: 5, TimeoutMs: 100})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := m.Stop(w.ID); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(10 * time.Second); m.activeCount() > 0; time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("not stopped")
			}
		}
		if err := m.Delete(w.ID, false); err != nil {
			t.Fatal(err)
		}
		_, err = m.Create(context.Background(), api.CreateRequest{Name: "w", Targets: []string{target, "nope://x"}})
		wantCode(t, err, api.CodeUnresolvable)
	}
	m := realManager(t)
	cycle(m)
	goroutines, files := runtime.NumGoroutine(), fds(t)
	for range 50 {
		cycle(m)
	}
	shutdown(t, m)
	if len(m.watches) != 0 {
		t.Errorf("%d watches are kept", len(m.watches))
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() >= goroutines+20 || fds(t) >= files+20 {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines, %d before; %d files, %d before", runtime.NumGoroutine(), goroutines, fds(t), files)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
