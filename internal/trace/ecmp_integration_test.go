//go:build integration

package trace

// Tests against the network namespaces built by test/netns/ecmp.sh. They are
// skipped unless MPING_IT_ECMP_TARGET is set; CI runs them as:
//
//	sudo test/netns/ecmp.sh up
//	go test -c -tags integration -o trace.test ./internal/trace/
//	sudo ip netns exec mping-c env MPING_IT_ECMP_TARGET=10.200.0.1 \
//	    MPING_IT_ECMP_HOP=2 ./trace.test -test.run IntegrationECMP -test.v
//
// Environment:
//
//	MPING_IT_ECMP_TARGET  destination behind the ECMP hop
//	MPING_IT_ECMP_HOP     TTL of the ECMP hop (default 2)
//	MPING_IT_ROUNDS / MPING_IT_INTERVAL / MPING_IT_PRIVILEGED as in integration_test.go

import (
	"context"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// runNetnsTrace traces target from the client namespace.
func runNetnsTrace(t *testing.T, target string, proto Proto) Result {
	t.Helper()
	rounds, _ := strconv.Atoi(envOr("MPING_IT_ROUNDS", "20"))
	interval, _ := time.ParseDuration(envOr("MPING_IT_INTERVAL", "200ms"))
	tr, err := New(target, Config{Interval: interval, Timeout: time.Second, NoDNS: true, Proto: proto})
	if err != nil {
		t.Fatalf("cannot trace: %v", err)
	}
	log := &traceLog{results: map[probeKey][]probeEvent{}}
	tr.onEvent = log.record
	if err := tr.Run(context.Background(), rounds); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	res := tr.Snapshot()
	t.Logf("proto=%s privileged=%v reached=%v hops=%d\n%s", res.Proto, res.Privileged, res.Reached, len(res.Hops), renderGrid(res, log))
	for _, h := range res.Hops {
		for _, r := range h.Responders {
			t.Logf("  hop %d responder %-12s recv=%d", h.TTL, r.Addr, r.Recv)
		}
	}
	checkSocket(t, res)
	return res
}

func TestIntegrationECMP(t *testing.T) {
	target := os.Getenv("MPING_IT_ECMP_TARGET")
	if target == "" {
		t.Skip("MPING_IT_ECMP_TARGET not set (see test/netns/ecmp.sh)")
	}
	hop, _ := strconv.Atoi(envOr("MPING_IT_ECMP_HOP", "2"))

	run := func(t *testing.T, proto Proto) Result {
		t.Helper()
		res := runNetnsTrace(t, target, proto)
		if !res.Reached || len(res.Hops) <= hop {
			t.Fatalf("destination not reached behind hop %d: %+v", hop, res)
		}
		if dst := res.Hops[len(res.Hops)-1]; dst.Addr != target || dst.Loss() > 0 {
			t.Errorf("destination hop = %+v", dst)
		}
		return res
	}

	t.Run("udp sees every branch", func(t *testing.T) {
		res := run(t, ProtoUDP)
		if n := len(res.Hops[hop-1].Responders); n < 2 {
			t.Errorf("hop %d has %d responder(s), want >= 2 (ECMP branches)", hop, n)
		}
		if h := res.Hops[hop-1]; h.Loss() > 0 {
			t.Errorf("hop %d loss = %.1f%%", hop, h.Loss())
		}
	})
	t.Run("icmp follows one branch", func(t *testing.T) {
		res := run(t, ProtoICMP)
		for _, h := range res.Hops {
			if n := len(h.Responders); n != 1 {
				t.Errorf("hop %d has %d responders, want exactly 1", h.TTL, n)
			}
		}
	})
}

// Every router of the path and what ends it is seen, whatever socket the
// trace uses: with IPv4 and IPv6, up to the destination and up to a router
// that reports it unreachable. The addresses are those of test/netns/ecmp.sh.
func TestIntegrationECMPPath(t *testing.T) {
	if os.Getenv("MPING_IT_ECMP_TARGET") == "" {
		t.Skip("MPING_IT_ECMP_TARGET not set (see test/netns/ecmp.sh)")
	}
	for _, tt := range []struct {
		name, target string
		hops         [][]string // possible responders of each hop
		limited      bool       // the router answers about once per second
	}{
		{"v4", "10.200.0.1", [][]string{{"10.100.0.1"}, {"10.101.0.2", "10.102.0.2"}, {"10.200.0.1"}}, false},
		{"v6", "fd00:200::1", [][]string{{"fd00:100::1"}, {"fd00:101::2"}, {"fd00:200::1"}}, false},
		{"v6 unreachable", "fd00:201::1", [][]string{{"fd00:100::1"}}, false},
		{"v6 administratively prohibited", "fd00:202::1", [][]string{{"fd00:100::1"}}, false},
		// Linux limits the rate of the errors for unreachable and prohibit
		// routes per sender (net.ipv4.route.error_cost, not per namespace),
		// so the answer may be to a probe with any TTL
		{"v4 host unreachable", "10.201.0.1", [][]string{{"10.100.0.1"}}, true},
		{"v4 administratively prohibited", "10.202.0.1", [][]string{{"10.100.0.1"}}, true},
	} {
		for _, proto := range []Proto{ProtoICMP, ProtoUDP} {
			t.Run(tt.name+"/"+string(proto), func(t *testing.T) {
				if !tt.limited {
					t.Parallel()
				}
				res := runNetnsTrace(t, tt.target, proto)
				if !res.Reached || (len(res.Hops) != len(tt.hops) && !tt.limited) {
					t.Fatalf("reached=%v with %d hops, want the path to end at hop %d", res.Reached, len(res.Hops), len(tt.hops))
				}
				for i, h := range res.Hops {
					want := tt.hops[min(i, len(tt.hops)-1)]
					if (h.Recv == 0 || h.Loss() > 0) && !tt.limited {
						t.Errorf("hop %d: %d of %d probes answered", h.TTL, h.Recv, h.Sent)
					}
					for _, r := range h.Responders {
						if !slices.Contains(want, r.Addr) {
							t.Errorf("hop %d answered by %s, want %v", h.TTL, r.Addr, want)
						}
					}
				}
			})
		}
	}
}
