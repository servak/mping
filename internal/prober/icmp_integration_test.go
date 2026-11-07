//go:build integration

package prober

// ICMP prober Start/Stop against loopback. The prober always opens a raw
// socket, so it needs root or cap_net_raw. Run with:
//
//	go test -tags integration -run Integration -v ./internal/prober/
//
// Environment (same conventions as internal/trace):
//
//	MPING_IT_STRICT     "1" fails instead of skipping when the raw socket
//	                    cannot be opened (CI grants cap_net_raw)
//	MPING_IT_PRIVILEGED "0" skips: the prober has no datagram fallback

import (
	"os"
	"testing"
	"time"
)

func TestIntegrationICMPProberLifecycle(t *testing.T) {
	if os.Getenv("MPING_IT_PRIVILEGED") == "0" {
		t.Skip("ICMP prober only supports raw sockets")
	}
	cases := []struct {
		version ProbeType
		target  string
	}{
		{ICMPV4, "127.0.0.1"},
		{ICMPV6, "::1"},
	}
	for _, tc := range cases {
		t.Run(string(tc.version), func(t *testing.T) {
			p, err := NewICMPProber(tc.version, &ICMPConfig{Body: "mping-it"}, string(tc.version))
			if err != nil {
				if os.Getenv("MPING_IT_STRICT") == "1" {
					t.Fatalf("NewICMPProber: %v", err)
				}
				t.Skipf("cannot open raw ICMP socket: %v", err)
			}
			if err := p.Accept(string(tc.version) + "://" + tc.target); err != nil {
				t.Fatal(err)
			}

			res := runLifecycle(t, p, []string{tc.target}, 100*time.Millisecond, 500*time.Millisecond)
			for _, e := range res.events {
				if e.Result != SUCCESS {
					continue
				}
				if e.Details == nil || e.Details.ICMP == nil {
					t.Fatalf("SUCCESS without ICMP details: %+v", e)
				}
				if e.Details.ICMP.Payload != "mping-it" {
					t.Errorf("payload = %q, want %q", e.Details.ICMP.Payload, "mping-it")
				}
			}
		})
	}
}
