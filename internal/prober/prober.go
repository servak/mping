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
}

type Prober interface {
	Accept(target string) error
	Start(chan *Event, time.Duration, time.Duration) error
	Stop()
}
