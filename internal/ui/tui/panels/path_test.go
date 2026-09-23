package panels

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
)

func TestFormatPath(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]

	if got := FormatPath("", nil, nil, &theme); !strings.Contains(got, "Select a host") {
		t.Errorf("no target: %q", got)
	}
	if got := FormatPath("h", nil, errors.New("permission denied"), &theme); !strings.Contains(got, "permission denied") {
		t.Errorf("error state: %q", got)
	}
	if got := FormatPath("h", nil, nil, &theme); !strings.Contains(got, "Starting") {
		t.Errorf("starting state: %q", got)
	}

	res := &trace.Result{
		Dst: net.ParseIP("192.0.2.10"), Rounds: 3, Reached: true, Privileged: true,
		Hops: []trace.Hop{
			{TTL: 1, Addr: "192.0.2.1", Name: strings.Repeat("very-long-router-name.", 4), Sent: 3, Recv: 3, Avg: time.Millisecond},
			{TTL: 2, Sent: 3},
			{TTL: 3, Addr: "192.0.2.10", Others: 2, Sent: 3, Recv: 2, Avg: 3 * time.Millisecond},
		},
	}
	got := FormatPath("tcp://[example]:443", res, nil, &theme)
	for _, want := range []string{"tcp://[example[]:443 (192.0.2.10)", "· 3 rounds", "???", "192.0.2.10 +2", "33.3%", "…"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatPath missing %q:\n%s", want, got)
		}
	}
}

func TestFormatPathTitleAndRateLimitNote(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	res := &trace.Result{
		Dst: net.ParseIP("192.0.2.10"), Rounds: 2, Reached: true, Privileged: true,
		Hops: []trace.Hop{
			{TTL: 1, Addr: "192.0.2.1", Sent: 4, Recv: 1},
			{TTL: 2, Addr: "192.0.2.10", Sent: 4, Recv: 4},
		},
	}
	got := FormatPath("example.com(192.0.2.10)", res, nil, &theme)
	if strings.Count(got, "192.0.2.10") != 2 { // title once + hop 2
		t.Errorf("destination IP should not be repeated in the title:\n%s", got)
	}
	if !strings.Contains(got, "rate limiting") {
		t.Errorf("expected rate limiting note:\n%s", got)
	}
}

func TestFormatPathWaitingAndRateLimitColors(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	res := &trace.Result{
		Dst: net.ParseIP("192.0.2.10"), Rounds: 5, Reached: true, Privileged: true,
		Hops: []trace.Hop{
			{TTL: 1, Sent: 0}, // probe in flight
			{TTL: 2, Addr: "192.0.2.2", Sent: 5, Recv: 1},  // rate limited router
			{TTL: 3, Addr: "192.0.2.10", Sent: 5, Recv: 5}, // clean destination
		},
	}
	got := FormatPath("192.0.2.10", res, nil, &theme)
	if !strings.Contains(got, "waiting") {
		t.Errorf("in-flight hop should say waiting:\n%s", got)
	}
	if strings.Contains(got, "["+theme.Error+"]") {
		t.Errorf("rate limited hop must not use the error color:\n%s", got)
	}

	// Loss that continues to the destination is real and stays red
	res.Hops[2].Recv = 3
	if got := FormatPath("192.0.2.10", res, nil, &theme); !strings.Contains(got, "["+theme.Error+"]") {
		t.Errorf("real loss should use the error color:\n%s", got)
	}
}
