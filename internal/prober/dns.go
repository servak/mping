package prober

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const (
	DNS ProbeType = "dns"
)

type DNSTarget struct {
	Server         string
	Port           int
	Domain         string
	RecordType     string
	UseTCP         bool
	ServerIP       string // Pre-resolved server IP
	OriginalTarget string // Original target string for display
}

type (
	DNSProber struct {
		targets []*DNSTarget
		config  *DNSConfig
		prefix  string
		loop    *runLoop
	}

	DNSConfig struct {
		Server           string `yaml:"server"`
		Port             int    `yaml:"port,omitempty"`
		RecordType       string `yaml:"record_type"`
		UseTCP           bool   `yaml:"use_tcp,omitempty"`
		RecursionDesired bool   `yaml:"recursion_desired,omitempty"`
		ExpectCodes      string `yaml:"expect_codes"` // DNS response codes: "0", "0-5", "0,2,3"
	}
)

// supportedDNSRecordTypes lists the record types mping can query. Both
// DNSConfig.Validate and target parsing use it.
var supportedDNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "PTR", "SOA", "SRV", "TXT"}

// Validate validates the DNS configuration
func (cfg *DNSConfig) Validate() error {
	if cfg.Server == "" {
		return fmt.Errorf("DNS server is required")
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid DNS server port: %d (must be 1-65535)", cfg.Port)
	}

	// Validate record type
	if !slices.Contains(supportedDNSRecordTypes, strings.ToUpper(cfg.RecordType)) {
		return fmt.Errorf("invalid DNS record type: %s (supported: %s)", cfg.RecordType, strings.Join(supportedDNSRecordTypes, ", "))
	}

	// Validate expect codes pattern if specified
	if cfg.ExpectCodes != "" {
		if !IsValidCodePattern(cfg.ExpectCodes) {
			return fmt.Errorf("invalid expect_codes pattern: %s", cfg.ExpectCodes)
		}
	}

	return nil
}

func NewDNSProber(cfg *DNSConfig, prefix string) *DNSProber {
	return &DNSProber{
		targets: make([]*DNSTarget, 0),
		config:  cfg,
		prefix:  prefix,
		loop:    newRunLoop(),
	}
}

// Accept parses DNS targets in format: dns://server[:port]/domain[/record_type] or dns:server[:port]/domain[/record_type]
func (p *DNSProber) Accept(target string) error {
	if _, ok := cutPrefix(target, p.prefix); !ok {
		return ErrNotAccepted
	}

	dnsTarget, err := p.parseTarget(target)
	if err != nil {
		return fmt.Errorf("invalid DNS target: %w", err)
	}

	// Validate DNS server by resolving its IP
	serverIP, err := net.ResolveIPAddr("ip", dnsTarget.Server)
	if err != nil {
		return fmt.Errorf("failed to resolve DNS server '%s': %w", dnsTarget.Server, err)
	}

	// Store complete DNSTarget with pre-resolved IP
	dnsTarget.ServerIP = serverIP.String()
	p.targets = append(p.targets, dnsTarget)

	// Unique and sorted targets
	slices.SortFunc(p.targets, func(a, b *DNSTarget) int {
		return cmp.Compare(a.OriginalTarget, b.OriginalTarget)
	})
	p.targets = slices.CompactFunc(p.targets, func(a, b *DNSTarget) bool {
		return a.OriginalTarget == b.OriginalTarget
	})
	return nil
}

func (p *DNSProber) parseTarget(target string) (*DNSTarget, error) {
	originalTarget := target
	target, _ = cutPrefix(target, p.prefix)

	// Split into server and query parts
	parts := strings.SplitN(target, "/", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid format, expected: %s://server/domain[/record_type] or %s:server/domain[/record_type]", p.prefix, p.prefix)
	}

	serverPart := parts[0]
	queryPart := parts[1]

	// Parse server and port
	server := p.config.Server
	port := p.config.Port
	if serverPart != "" {
		host, portStr, err := splitServerPort(serverPart)
		if err != nil {
			return nil, err
		}
		if host != "" {
			server = host
		}
		if portStr != "" {
			n, err := strconv.Atoi(portStr)
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: must be a number between 1 and 65535", portStr)
			}
			if n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid port %d: must be between 1 and 65535", n)
			}
			port = n
		}
	}

	// Parse domain and record type
	queryParts := strings.Split(queryPart, "/")
	domain := queryParts[0]
	recordType := p.config.RecordType
	if len(queryParts) > 1 && queryParts[1] != "" {
		recordType = queryParts[1]
	}
	recordType = strings.ToUpper(recordType)
	if !slices.Contains(supportedDNSRecordTypes, recordType) {
		return nil, fmt.Errorf("unsupported record type: %q (supported: %s)", recordType, strings.Join(supportedDNSRecordTypes, ", "))
	}
	return &DNSTarget{
		Server:         server,
		Port:           port,
		Domain:         domain,
		RecordType:     recordType,
		UseTCP:         p.config.UseTCP,
		OriginalTarget: originalTarget,
	}, nil
}

func (p *DNSProber) Start(result chan *Event, interval, timeout time.Duration) error {
	return runPeriodic(p.loop, result, DNS, interval, p.targets,
		func(t *DNSTarget) (string, string) { return t.OriginalTarget, t.OriginalTarget },
		func(t *DNSTarget) { p.sendProbe(result, t, timeout) })
}

// Stop stops probing and waits for Start to return. It is safe to call more than once.
func (p *DNSProber) Stop() {
	p.loop.stop()
}

func (p *DNSProber) sendProbe(result chan *Event, target *DNSTarget, timeout time.Duration) {
	now := time.Now()
	key := target.OriginalTarget
	result <- sentEvent(key, key, now)

	// Create DNS client
	c := new(dns.Client)
	c.Timeout = timeout
	if target.UseTCP {
		c.Net = "tcp"
	}

	// Create DNS query
	m := new(dns.Msg)
	// RecordType was validated against supportedDNSRecordTypes at parse time.
	m.SetQuestion(dns.Fqdn(target.Domain), dns.StringToType[target.RecordType])
	m.RecursionDesired = p.config.RecursionDesired

	// Send DNS query using pre-resolved server IP
	server := net.JoinHostPort(target.ServerIP, strconv.Itoa(target.Port))
	r, rtt, err := c.Exchange(m, server)
	if err != nil {
		result <- dnsErrorEvent(key, now, err)
		return
	}

	// A truncated UDP answer is incomplete: retry over TCP. The retry only gets
	// what is left of the timeout budget so the probe never exceeds it, and the
	// reported RTT is the total wall time of the probe (UDP attempt plus retry).
	if r.Truncated && !target.UseTCP {
		remaining := timeout - time.Since(now)
		if remaining <= 0 {
			result <- dnsErrorEvent(key, now, fmt.Errorf("DNS response truncated, no time left for TCP retry: %w", os.ErrDeadlineExceeded))
			return
		}
		tc := new(dns.Client)
		tc.Timeout = remaining
		tc.Net = "tcp"
		r2, _, err := tc.Exchange(m, server)
		if err != nil {
			result <- dnsErrorEvent(key, now, fmt.Errorf("DNS response truncated, TCP retry failed: %w", err))
			return
		}
		r, rtt = r2, time.Since(now)
	}

	// Check DNS response
	if !p.isExpectedResponseCode(r.Rcode) {
		result <- errorEvent(key, key, now, fmt.Errorf("DNS response code: %d (%s)", r.Rcode, dns.RcodeToString[r.Rcode]))
		return
	}

	// Success
	result <- successEvent(key, key, now, rtt, p.details(target, r))
}

// dnsErrorEvent is errorEvent, except that a timeout is recognised even when
// err wraps it (classifyErr only looks at the outermost error).
func dnsErrorEvent(key string, sent time.Time, err error) *Event {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return failureEvent(key, key, TIMEOUT, sent, 0, err.Error())
	}
	return errorEvent(key, key, sent, err)
}

// details describes a successful DNS response.
func (p *DNSProber) details(target *DNSTarget, resp *dns.Msg) *ProbeDetails {
	var answers []string
	for _, ans := range resp.Answer {
		answers = append(answers, ans.String())
	}

	return &ProbeDetails{
		ProbeType: "dns",
		DNS: &DNSDetails{
			Server:       target.Server,
			Port:         target.Port,
			Domain:       target.Domain,
			RecordType:   target.RecordType,
			ResponseCode: resp.Rcode,
			AnswerCount:  len(resp.Answer),
			Answers:      answers,
			UseTCP:       target.UseTCP,
		},
	}
}

// isExpectedResponseCode checks if the DNS response code matches the expected criteria
func (p *DNSProber) isExpectedResponseCode(rcode int) bool {
	// If ExpectCodes is specified, use it; otherwise default to success only (0)
	if p.config.ExpectCodes != "" {
		return MatchCode(rcode, p.config.ExpectCodes)
	}

	// Default: only accept successful responses (NOERROR = 0)
	return rcode == dns.RcodeSuccess
}
