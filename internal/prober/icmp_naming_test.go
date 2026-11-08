package prober

import (
	"net/url"
	"testing"

	"github.com/servak/mping/internal/trace"
)

// Echo IDs of the probers in one process are distinct and never 0.
func TestNextEchoIDDistinct(t *testing.T) {
	seen := make(map[int]bool)
	for range 1000 {
		id := nextEchoID()
		if id <= 0 || id > 0xffff {
			t.Fatalf("nextEchoID() = %d, out of range", id)
		}
		if seen[id] {
			t.Fatalf("nextEchoID() returned %d twice", id)
		}
		seen[id] = true
	}
}

// A custom ICMPv6 prober brackets bare addresses in its keys and names, so
// that URL-style parsing (as by the path tracer) finds the whole address.
func TestICMPCustomV6NameParses(t *testing.T) {
	p := newICMPProber(ICMPV6, newFakeICMPConn(), nil, "myv6")
	if err := p.Accept("myv6://2001:db8::1"); err != nil {
		t.Fatal(err)
	}
	got := p.targets["2001:db8::1"]
	if got.key != "myv6://[2001:db8::1]" || got.name != "myv6://[2001:db8::1]" {
		t.Fatalf("target = %+v, want bracketed key and name", got)
	}
	u, err := url.Parse(got.name)
	if err != nil || u.Hostname() != "2001:db8::1" || u.Port() != "" {
		t.Errorf("url.Parse(%q) = %v, %v", got.name, u, err)
	}
	if host, _ := trace.HostFromTarget(got.name); host != "2001:db8::1" {
		t.Errorf("trace.HostFromTarget(%q) = %q", got.name, host)
	}
}

// Plain targets are shown by their bare address even when a custom ICMP
// prober is the default; targets written with its scheme carry it.
func TestICMPCustomDefaultPlainTargets(t *testing.T) {
	config := map[string]*ProberConfig{
		"ping":   {Probe: ICMPV4, ICMP: &ICMPConfig{}},
		"icmpv4": {Probe: ICMPV4, ICMP: &ICMPConfig{}},
	}
	pm := NewProbeManager(config, "ping").(*probeManager)
	p := newICMPProber(ICMPV4, newFakeICMPConn(), nil, "ping")
	pm.probers["ping"] = p
	if err := pm.AddTargets("127.0.0.1", "ping://127.0.0.2", "ping:127.0.0.3"); err != nil {
		t.Fatal(err)
	}
	want := map[string]icmpTarget{
		"127.0.0.1": {key: "ping://127.0.0.1", name: "127.0.0.1"},
		"127.0.0.2": {key: "ping://127.0.0.2", name: "ping://127.0.0.2"},
		"127.0.0.3": {key: "ping://127.0.0.3", name: "ping://127.0.0.3"},
	}
	for ip, w := range want {
		if got := p.targets[ip]; got != w {
			t.Errorf("target %s = %+v, want %+v", ip, got, w)
		}
	}
}
