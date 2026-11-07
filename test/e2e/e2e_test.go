//go:build e2e

// Package e2e runs the real mping binary ("mping batch -o json") against
// servers started on loopback and checks the reported statistics.
//
//	go test -tags e2e -v ./test/e2e/
//
// Environment:
//
//	MPING_BIN       mping binary to test (default: built from ./cmd/mping)
//	MPING_E2E_ICMP  "require" fails ICMP cases that cannot open a raw socket
//	                instead of skipping them (CI grants cap_net_raw)
//	MPING_E2E_IPV6  "require" fails IPv6 cases when ::1 is unavailable
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	probeCount    = 3
	probeInterval = "300" // ms
)

var mpingBin string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	mpingBin = os.Getenv("MPING_BIN")
	if mpingBin == "" {
		dir, err := os.MkdirTemp("", "mping-e2e")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(dir)
		mpingBin = filepath.Join(dir, "mping")
		if runtime.GOOS == "windows" {
			mpingBin += ".exe"
		}
		build := exec.Command("go", "build", "-o", mpingBin, "github.com/servak/mping/cmd/mping")
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "building mping:", err)
			return 1
		}
	}
	if abs, err := filepath.Abs(mpingBin); err == nil {
		mpingBin = abs
	}
	return m.Run()
}

// targetReport mirrors the fields of shared.TargetReport that are checked
type targetReport struct {
	Host           string  `json:"host"`
	Sent           int     `json:"sent"`
	Success        int     `json:"success"`
	Fail           int     `json:"fail"`
	LossPercent    float64 `json:"loss_percent"`
	AvgRTTMs       float64 `json:"avg_rtt_ms"`
	LastFailReason string  `json:"last_fail_reason"`
}

type result struct {
	reports  []targetReport
	exitCode int
	stdout   string
	stderr   string
}

// options for one mping invocation
type options struct {
	config  string   // YAML written to a temp file and passed with -c
	timeout string   // ms (default 1000)
	extra   []string // extra flags
}

// mping runs "mping batch -o json" and decodes the report. Without a config
// a nonexistent path is passed so ~/.mping.yml never affects the test.
func mping(t *testing.T, opts options, targets ...string) result {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "absent.yml")
	if opts.config != "" {
		cfgPath = filepath.Join(dir, "mping.yml")
		if err := os.WriteFile(cfgPath, []byte(opts.config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	timeout := opts.timeout
	if timeout == "" {
		timeout = "1000"
	}
	args := []string{"batch", "-o", "json", "-c", cfgPath,
		"--count", fmt.Sprint(probeCount), "--interval", probeInterval, "--timeout", timeout}
	args = append(args, opts.extra...)
	args = append(args, targets...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, mpingBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("running %s %s: %v", mpingBin, strings.Join(args, " "), err)
	}
	t.Logf("mping %s -> exit %d", strings.Join(args, " "), res.exitCode)
	if res.exitCode != 0 && res.exitCode != 2 {
		// No report was written (usage, config or socket error)
		return res
	}
	if err := json.Unmarshal(stdout.Bytes(), &res.reports); err != nil {
		t.Fatalf("decoding JSON report: %v\nstdout:\n%s\nstderr:\n%s", err, res.stdout, res.stderr)
	}
	return res
}

// mustSucceed asserts exit code 0 and that every target answered every probe.
func (r result) mustSucceed(t *testing.T, wantTargets int) {
	t.Helper()
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if len(r.reports) != wantTargets {
		t.Fatalf("got %d target reports, want %d:\n%s", len(r.reports), wantTargets, r.stdout)
	}
	for _, rep := range r.reports {
		if rep.Sent != probeCount || rep.Success != rep.Sent || rep.Fail != 0 || rep.LossPercent != 0 {
			t.Errorf("%s: sent=%d success=%d fail=%d loss=%.1f%% (reason %q), want %d/%d successful",
				rep.Host, rep.Sent, rep.Success, rep.Fail, rep.LossPercent, rep.LastFailReason, probeCount, probeCount)
		}
		if rep.AvgRTTMs <= 0 {
			t.Errorf("%s: avg_rtt_ms = %v, want > 0", rep.Host, rep.AvgRTTMs)
		}
	}
}

// mustFail asserts that every probe of every target failed and that
// --max-loss turned that into exit code 2.
func (r result) mustFail(t *testing.T, wantTargets int, reason string) {
	t.Helper()
	if r.exitCode != 2 {
		t.Fatalf("exit code = %d, want 2 (max-loss exceeded)\nstdout:\n%s\nstderr:\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if len(r.reports) != wantTargets {
		t.Fatalf("got %d target reports, want %d:\n%s", len(r.reports), wantTargets, r.stdout)
	}
	for _, rep := range r.reports {
		if rep.Sent != probeCount || rep.Success != 0 || rep.Fail != rep.Sent {
			t.Errorf("%s: sent=%d success=%d fail=%d, want all %d probes to fail",
				rep.Host, rep.Sent, rep.Success, rep.Fail, probeCount)
		}
		if !strings.Contains(strings.ToLower(rep.LastFailReason), reason) {
			t.Errorf("%s: last_fail_reason = %q, want it to contain %q", rep.Host, rep.LastFailReason, reason)
		}
	}
}

var failOnLoss = []string{"--max-loss", "0"}

// requireICMP skips (or fails, with MPING_E2E_ICMP=require) when the binary
// cannot open a raw ICMP socket.
func requireICMP(t *testing.T, r result) {
	t.Helper()
	if r.exitCode == 1 && strings.Contains(r.stdout+r.stderr, "not permitted") {
		msg := "mping cannot open a raw ICMP socket (run as root or grant cap_net_raw):\n" + r.stderr
		if os.Getenv("MPING_E2E_ICMP") == "require" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
}

func requireIPv6(t *testing.T) {
	t.Helper()
	if ipv6LoopbackAvailable() {
		return
	}
	if os.Getenv("MPING_E2E_IPV6") == "require" {
		t.Fatal("IPv6 loopback ::1 is not available")
	}
	t.Skip("IPv6 loopback ::1 is not available")
}

func TestICMPv4(t *testing.T) {
	t.Parallel()
	r := mping(t, options{}, "127.0.0.1")
	requireICMP(t, r)
	r.mustSucceed(t, 1)
}

func TestICMPv6(t *testing.T) {
	t.Parallel()
	requireIPv6(t)
	r := mping(t, options{}, "icmpv6:::1")
	requireICMP(t, r)
	r.mustSucceed(t, 1)
}

func TestHTTP(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, false)
	mping(t, options{}, srv.URL).mustSucceed(t, 1)
}

func TestHTTPS(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, true) // self-signed: verification skipped by default
	mping(t, options{}, srv.URL).mustSucceed(t, 1)
}

func TestTCP(t *testing.T) {
	t.Parallel()
	addr := newTCPServer(t, "tcp4", "127.0.0.1:0")
	mping(t, options{}, "tcp://"+addr.String()).mustSucceed(t, 1)
}

// URL targets with an IPv6 literal ("tcp://[::1]:port", "http://[::1]:port")
// cannot be used yet: target bracket expansion ("web[1-3]") turns "[::1]"
// into "::1" before the prober sees it.
func TestTCPIPv6Literal(t *testing.T) {
	t.Parallel()
	requireIPv6(t)
	addr := newTCPServer(t, "tcp6", "[::1]:0")
	r := mping(t, options{}, "tcp://"+addr.String())
	if r.exitCode == 1 && strings.Contains(r.stderr, "tcp://::1:") {
		t.Skip("known issue: bracket expansion strips the brackets of IPv6 literals: " + strings.TrimSpace(r.stderr))
	}
	r.mustSucceed(t, 1)
}

func TestDNS(t *testing.T) {
	t.Parallel()
	server := newDNSServer(t, "udp")
	mping(t, options{},
		"dns://"+server+"/e2e.test",
		"dns://"+server+"/e2e.test/AAAA",
	).mustSucceed(t, 2)
}

func TestDNSOverTCP(t *testing.T) {
	t.Parallel()
	server := newDNSServer(t, "tcp")
	cfg := `
prober:
  dns-tcp:
    probe: dns
    dns:
      server: 127.0.0.1
      port: 53
      use_tcp: true
      record_type: A
      expect_codes: "0"
`
	mping(t, options{config: cfg}, "dns-tcp://"+server+"/e2e.test").mustSucceed(t, 1)
}

func TestNTP(t *testing.T) {
	t.Parallel()
	mping(t, options{}, "ntp://"+newNTPServer(t)).mustSucceed(t, 1)
}

// All probers in a single run, as a user would mix them.
func TestMixedProtocols(t *testing.T) {
	t.Parallel()
	httpSrv := newHTTPServer(t, false)
	httpsSrv := newHTTPServer(t, true)
	targets := []string{
		httpSrv.URL,
		httpsSrv.URL,
		"tcp://" + newTCPServer(t, "tcp4", "127.0.0.1:0").String(),
		"dns://" + newDNSServer(t, "udp") + "/e2e.test",
		"ntp://" + newNTPServer(t),
	}
	mping(t, options{}, targets...).mustSucceed(t, len(targets))
}

// A partial config that only overrides https.expect_codes must keep the
// default TLS settings (skip_verify) and still probe over HTTPS.
func TestPartialHTTPSConfig(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, true)
	cfg := `
prober:
  https:
    http:
      expect_codes: "200"
`
	mping(t, options{config: cfg}, srv.URL).mustSucceed(t, 1)
}

// A custom prober prefix with an explicit scheme and custom headers.
func TestCustomHTTPProber(t *testing.T) {
	t.Parallel()
	httpsSrv := newHTTPServer(t, true)
	httpSrv := newHTTPServer(t, false)
	cfg := `
prober:
  api-check:
    probe: http
    http:
      expect_codes: "200"
      expect_body: healthy
      headers:
        X-E2E: "yes"
`
	httpsHost := strings.TrimPrefix(httpsSrv.URL, "https://")
	httpHost := strings.TrimPrefix(httpSrv.URL, "http://")
	mping(t, options{config: cfg},
		"api-check://https://"+httpsHost+"/health",
		"api-check://"+httpHost+"/health", // no scheme: probe: http decides
	).mustSucceed(t, 2)
}

// A custom prober with probe: https and no tls block uses HTTPS.
func TestCustomHTTPSProber(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, true)
	cfg := `
prober:
  secure-check:
    probe: https
    http:
      expect_codes: "200-299"
`
	mping(t, options{config: cfg}, "secure-check://"+strings.TrimPrefix(srv.URL, "https://")).mustSucceed(t, 1)
}

// --- failure cases ---

func TestTCPClosedPortFails(t *testing.T) {
	t.Parallel()
	r := mping(t, options{extra: failOnLoss}, "tcp://"+closedTCPPort(t))
	r.mustFail(t, 1, "refused")
}

func TestHTTPUnexpectedStatusFails(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, false)
	mping(t, options{extra: failOnLoss}, srv.URL+"/status/500").mustFail(t, 1, "500")
}

func TestHTTPSVerifyFailsWithSelfSignedCert(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(t, true)
	cfg := `
prober:
  https:
    http:
      tls:
        skip_verify: false
`
	mping(t, options{config: cfg, extra: failOnLoss}, srv.URL).mustFail(t, 1, "certificate")
}

func TestDNSNXDomainFails(t *testing.T) {
	t.Parallel()
	server := newDNSServer(t, "udp")
	mping(t, options{extra: failOnLoss}, "dns://"+server+"/missing.e2e.test").mustFail(t, 1, "nxdomain")
}

func TestNTPNoServerFails(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	// Linux reports ICMP port unreachable as "connection refused"; other
	// systems may just time out.
	r := mping(t, options{timeout: "200", extra: failOnLoss}, "ntp://"+addr)
	r.mustFail(t, 1, "")
}

func TestICMPUnreachableTimesOut(t *testing.T) {
	t.Parallel()
	// 192.0.2.0/24 (TEST-NET-1) is never routed on the Internet
	r := mping(t, options{timeout: "200", extra: failOnLoss}, "192.0.2.1")
	requireICMP(t, r)
	r.mustFail(t, 1, "")
}
