package prober

import (
	"context"
	"errors"
	"net"
	"time"
)

type (
	reason    uint8
	ProbeType string
)

const (
	REGISTER reason = iota
	SENT
	SUCCESS
	TIMEOUT
	FAILED

	maxPacketSize = 1500
)

// Common errors
var ErrNotAccepted = errors.New("target not accepted by this prober")

type Event struct {
	Key         string
	DisplayName string
	Result      reason
	SentTime    time.Time
	Rtt         time.Duration
	Message     string
	Details     *ProbeDetails // Added: detailed information
	// ProbeType is the kind of prober (icmpv4, http, tcp, ...) that owns the
	// target. It is set on REGISTER events.
	ProbeType ProbeType
}

type Prober interface {
	// Accept adds a target, which carries the scheme of the prober, before
	// Start.
	Accept(target string) error
	Start(chan *Event, time.Duration, time.Duration) error
	Stop()

	// prepare does the part of Accept that takes time or may fail: it checks
	// target and looks up its name with r, until ctx is done. plain tells that
	// the user wrote the target without a scheme, so that it went to the
	// default prober. prepare changes nothing, so it may be called
	// concurrently, also with Stop; the returned function, which adds the
	// target, may not.
	prepare(ctx context.Context, r resolver, target string, plain bool) (add func(), err error)
}

// accept is the Accept of all probers.
func accept(p Prober, target string) error {
	add, err := p.prepare(context.Background(), net.DefaultResolver, target, false)
	if err != nil {
		return err
	}
	add()
	return nil
}
