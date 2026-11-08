package prober

import (
	"net/url"
	"testing"

	"github.com/servak/mping/internal/icmpid"
	"github.com/servak/mping/internal/trace"
)

// Probers and path tracers draw their echo IDs from one process-wide
// allocator: the IDs held at once are distinct and never 0, also while
// tracers churn (they restart on every target change), and a stopped
// prober releases its ID.
func TestEchoIDsDisjointFromTracer(t *testing.T) {
	var probers []*ICMPProber
	held := make(map[uint16]bool)
	for i := range 200 {
		p := newICMPProber(ICMPV4, newFakeICMPConn(), nil, "icmpv4")
		probers = append(probers, p)
		id := uint16(p.runID)
		if id == 0 || p.runID > 0xffff || held[id] {
			t.Fatalf("prober echo ID %d: zero, out of range or duplicate", p.runID)
		}
		held[id] = true
		// a tracer starting and stopping between probers (trace uses
		// icmpid.Acquire / Release for its transports)
		tr := icmpid.Acquire()
		if tr == 0 || held[tr] {
			t.Fatalf("tracer echo ID %#x: zero or held by a prober", tr)
		}
		if i%2 == 0 {
			icmpid.Release(tr)
		} else {
			held[tr] = true
			defer icmpid.Release(tr)
		}
	}
	for _, p := range probers {
		p.Stop()
		if icmpid.InUse(uint16(p.runID)) {
			t.Fatalf("echo ID %d still held after Stop", p.runID)
		}
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
