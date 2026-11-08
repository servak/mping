//go:build integration

package trace

// ECMP test against the network namespaces built by test/netns/ecmp.sh. It
// is skipped unless MPING_IT_ECMP_TARGET is set; CI runs it as:
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
	"strconv"
	"testing"
	"time"
)

func TestIntegrationECMP(t *testing.T) {
	target := os.Getenv("MPING_IT_ECMP_TARGET")
	if target == "" {
		t.Skip("MPING_IT_ECMP_TARGET not set (see test/netns/ecmp.sh)")
	}
	hop, _ := strconv.Atoi(envOr("MPING_IT_ECMP_HOP", "2"))
	rounds, _ := strconv.Atoi(envOr("MPING_IT_ROUNDS", "20"))
	interval, _ := time.ParseDuration(envOr("MPING_IT_INTERVAL", "200ms"))

	run := func(t *testing.T, proto Proto) Result {
		t.Helper()
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
		switch os.Getenv("MPING_IT_PRIVILEGED") {
		case "1":
			if !res.Privileged {
				t.Errorf("traced without a raw socket")
			}
		case "0":
			if res.Privileged {
				t.Errorf("traced with a raw socket, want the unprivileged fallback")
			}
		}
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
