package prober

import "time"

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

type Event struct {
	Target   string
	Result   reason
	SentTime time.Time
	Rtt      time.Duration
	Message  string
}

type Prober interface {
	Start(chan *Event, time.Duration, time.Duration) error
	Stop()
}
