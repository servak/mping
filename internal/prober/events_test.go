package prober

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestCutPrefix(t *testing.T) {
	tests := []struct {
		target, prefix string
		want           string
		ok             bool
	}{
		{"tcp://host:80", "tcp", "host:80", true},
		{"tcp:host:80", "tcp", "host:80", true},
		{"my-ping://1.1.1.1", "my-ping", "1.1.1.1", true},
		{"dns://8.8.8.8/example.com/A", "dns", "8.8.8.8/example.com/A", true},
		{"udp://host:80", "tcp", "udp://host:80", false},
		{"tcpx://host", "tcp", "tcpx://host", false},
		{"host", "tcp", "host", false},
	}
	for _, tt := range tests {
		got, ok := cutPrefix(tt.target, tt.prefix)
		if got != tt.want || ok != tt.ok {
			t.Errorf("cutPrefix(%q, %q) = %q, %v; want %q, %v", tt.target, tt.prefix, got, ok, tt.want, tt.ok)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassifyErr(t *testing.T) {
	var netTimeout net.Error = timeoutErr{}
	tests := []struct {
		name string
		err  error
		want reason
	}{
		{"net timeout", netTimeout, TIMEOUT},
		{"op error timeout", &net.OpError{Op: "dial", Err: netTimeout}, TIMEOUT},
		{"refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, FAILED},
		{"plain error", errors.New("boom"), FAILED},
		{"context deadline", context.DeadlineExceeded, TIMEOUT},
		{"wrapped timeout", fmt.Errorf("read: %w", netTimeout), TIMEOUT},
		{"wrapped deadline", fmt.Errorf("read: %w", context.DeadlineExceeded), TIMEOUT},
	}
	for _, tt := range tests {
		if got := classifyErr(tt.err); got != tt.want {
			t.Errorf("%s: classifyErr() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestErrorEvent(t *testing.T) {
	e := errorEvent("k", "name", time.Now(), timeoutErr{})
	if e.Result != TIMEOUT || e.Message != "i/o timeout" || e.Key != "k" || e.DisplayName != "name" || e.Rtt != 0 {
		t.Errorf("errorEvent() = %+v", e)
	}
}
