package tui

import (
	"context"
	"sync"
	"time"

	"github.com/servak/mping/internal/trace"
)

// minPathInterval keeps path tracing polite: routers rate-limit ICMP Time
// Exceeded generation, and probing them faster shows fake loss.
const minPathInterval = time.Second

// pathTracer runs a background trace for the host selected in the TUI and
// restarts it when the selection changes.
type pathTracer struct {
	cfg trace.Config

	mu     sync.Mutex
	target string
	tracer *trace.Tracer
	err    error
	cancel context.CancelFunc
}

func newPathTracer(interval, timeout time.Duration) *pathTracer {
	return &pathTracer{cfg: trace.Config{
		Interval: max(interval, minPathInterval),
		Timeout:  max(timeout, minPathInterval),
	}}
}

// Follow traces target, restarting the trace if the target changed.
// Socket setup and name resolution happen off the UI goroutine.
func (p *pathTracer) Follow(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if target == p.target {
		return
	}
	p.stopLocked()
	p.target = target
	if target == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go func() {
		tr, err := trace.New(target, p.cfg)
		p.mu.Lock()
		if ctx.Err() != nil {
			p.mu.Unlock()
			if tr != nil {
				tr.Close()
			}
			return
		}
		p.tracer, p.err = tr, err
		p.mu.Unlock()
		if err != nil {
			return
		}
		if err := tr.Run(ctx, 0); err != nil {
			p.mu.Lock()
			if ctx.Err() == nil {
				p.err = err
			}
			p.mu.Unlock()
		}
	}()
}

// Stop stops tracing
func (p *pathTracer) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.target = ""
}

func (p *pathTracer) stopLocked() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.tracer = nil
	p.err = nil
}

// State returns the current target, trace snapshot (nil while starting) and error
func (p *pathTracer) State() (string, *trace.Result, error) {
	p.mu.Lock()
	target, tr, err := p.target, p.tracer, p.err
	p.mu.Unlock()
	if tr == nil {
		return target, nil, err
	}
	res := tr.Snapshot()
	return target, &res, err
}
