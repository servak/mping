package prober

import (
	"errors"
	"sync"
	"time"
)

var errAlreadyStarted = errors.New("prober already started")

// runLoop is the Start/Stop lifecycle shared by all probers.
//
// Start calls begin, runs until stopping() is closed and calls finish when
// all of its work (including in-flight probes) is done. Stop calls stop,
// which may be called any number of times, before or after Start, and waits
// for a running Start to finish.
type runLoop struct {
	mu      sync.Mutex
	started bool
	stopped bool
	exit    chan struct{}
	done    chan struct{}
}

func newRunLoop() *runLoop {
	return &runLoop{
		exit: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// begin marks the loop as running. It returns false (and no error) when Stop
// was already called, in which case Start must return without doing anything.
func (l *runLoop) begin() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return false, errAlreadyStarted
	}
	if l.stopped {
		return false, nil
	}
	l.started = true
	return true, nil
}

// finish reports that Start has finished. Only the Start whose begin
// returned true may call it.
func (l *runLoop) finish() {
	close(l.done)
}

// stopping is closed once Stop has been called.
func (l *runLoop) stopping() <-chan struct{} {
	return l.exit
}

// stop signals the loop to exit and waits until a running Start has
// finished. It reports whether Start had begun, so that callers can release
// resources Start would otherwise have released itself.
func (l *runLoop) stop() bool {
	l.mu.Lock()
	if !l.stopped {
		l.stopped = true
		close(l.exit)
	}
	started := l.started
	l.mu.Unlock()
	if started {
		<-l.done
	}
	return started
}

// register announces a target before it is probed.
func register(r chan<- *Event, key, displayName string) {
	r <- &Event{Key: key, DisplayName: displayName, Result: REGISTER}
}

// runPeriodic is the Start of the probers that probe each target on its own:
// it registers the targets, probes all of them right away and then on every
// tick (one goroutine per target per round) until Stop, and returns once the
// in-flight probes have finished.
func runPeriodic[T any](l *runLoop, r chan<- *Event, interval time.Duration, targets []T, id func(T) (key, displayName string), probe func(T)) error {
	ok, err := l.begin()
	if !ok {
		return err
	}
	defer l.finish()

	for _, t := range targets {
		key, name := id(t)
		register(r, key, name)
	}

	// Only this goroutine adds to and waits on wg.
	var wg sync.WaitGroup
	defer wg.Wait()
	round := func() {
		for _, t := range targets {
			wg.Go(func() { probe(t) })
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	round()
	for {
		select {
		case <-l.stopping():
			return nil
		case <-ticker.C:
			round()
		}
	}
}
