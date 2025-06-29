package prober

import (
	"errors"
	"time"
)

type (
	reason    uint8
	ProbeType string
)

const (
	SENT reason = iota
	SUCCESS
	TIMEOUT
	FAILED

	maxPacketSize = 1500
)

// Common errors
var ErrNotAccepted = errors.New("target not accepted by this prober")

type Event struct {
	Target   string
	Result   reason
	SentTime time.Time
	Rtt      time.Duration
	Message  string
}

type Prober interface {
	Accept(target string) (displayName string, err error)
	HasTargets() bool
	Start(chan *Event, time.Duration, time.Duration) error
	Stop()
}
