package prober

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	DNS ProbeType = "dns"
)

type (
	DNSProber struct {
		targets  []*DNSTarget
		config   *DNSConfig
		prefix   string
		exitChan chan bool
		wg       sync.WaitGroup
	}

	DNSConfig struct {
		Server     string `yaml:"server"`
		Port       int    `yaml:"port"`
		RecordType string `yaml:"record_type"`
		UseTCP     bool   `yaml:"use_tcp"`
		Timeout    string `yaml:"timeout"`
	}

	DNSTarget struct {
		Server     string
		Port       int
		Domain     string
		RecordType string
		UseTCP     bool
		ServerIP   string // Pre-resolved server IP
		Key        string // For Event.Target matching
	}
)

func NewDNSProber(cfg *DNSConfig, prefix string) *DNSProber {
	return &DNSProber{
		targets:  make([]*DNSTarget, 0),
		config:   cfg,
		prefix:   prefix,
		exitChan: make(chan bool),
	}
}

// Accept parses DNS targets in format: dns://server[:port]/domain[/record_type] or dns:server[:port]/domain[/record_type]
func (p *DNSProber) Accept(target string) error {
	// Check if it's new format (dns://...) or legacy format (dns:...)
	if !strings.HasPrefix(target, p.prefix+"://") && !strings.HasPrefix(target, p.prefix+":") {
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

	// Create unique key for Event.Target matching
	key := fmt.Sprintf("%s:%d|%s|%s|%t", serverIP.String(), dnsTarget.Port, dnsTarget.Domain, dnsTarget.RecordType, dnsTarget.UseTCP)

	// Store complete DNSTarget with pre-resolved IP
	dnsTarget.ServerIP = serverIP.String()
	dnsTarget.Key = key
	p.targets = append(p.targets, dnsTarget)

	return nil
}

func (p *DNSProber) parseTarget(target string) (*DNSTarget, error) {
	// Remove dns:// or dns: prefix
	if strings.HasPrefix(target, p.prefix+"://") {
		target = strings.TrimPrefix(target, p.prefix+"://")
	} else if strings.HasPrefix(target, p.prefix+":") {
		target = strings.TrimPrefix(target, p.prefix+":")
	}

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
		if strings.Contains(serverPart, ":") {
			host, portStr, err := net.SplitHostPort(serverPart)
			if err != nil {
				return nil, fmt.Errorf("invalid server:port format: %w", err)
			}
			server = host
			if p, err := strconv.Atoi(portStr); err == nil {
				port = p
			}
		} else {
			server = serverPart
		}
	}

	// Parse domain and record type
	queryParts := strings.Split(queryPart, "/")
	domain := queryParts[0]
	recordType := p.config.RecordType

	if len(queryParts) > 1 && queryParts[1] != "" {
		recordType = strings.ToUpper(queryParts[1])
	}

	// Default values
	if server == "" {
		server = "8.8.8.8"
	}
	if port == 0 {
		port = 53
	}
	if recordType == "" {
		recordType = "A"
	}

	return &DNSTarget{
		Server:     server,
		Port:       port,
		Domain:     domain,
		RecordType: recordType,
		UseTCP:     p.config.UseTCP,
	}, nil
}

func (p *DNSProber) Start(result chan *Event, interval, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for _, target := range p.targets {
			go p.sendProbe(result, target, timeout)
		}
		for {
			select {
			case <-p.exitChan:
				ticker.Stop()
				return
			case <-ticker.C:
				for _, target := range p.targets {
					go p.sendProbe(result, target, timeout)
				}
			}
		}
	}()
	p.wg.Wait()
	return nil
}

func (p *DNSProber) Stop() {
	close(p.exitChan)
	p.wg.Wait()
}

func (p *DNSProber) sendProbe(result chan *Event, target *DNSTarget, timeout time.Duration) {
	p.wg.Add(1)
	defer p.wg.Done()

	now := time.Now()
	p.sent(result, target, now)

	// Create DNS client
	c := new(dns.Client)
	c.Timeout = timeout
	if target.UseTCP {
		c.Net = "tcp"
	}

	// Create DNS query
	m := new(dns.Msg)
	qtype := dns.StringToType[target.RecordType]
	if qtype == 0 {
		p.failed(result, target, now, fmt.Errorf("unsupported record type: %s", target.RecordType))
		return
	}

	m.SetQuestion(dns.Fqdn(target.Domain), qtype)

	// Send DNS query using pre-resolved server IP
	server := fmt.Sprintf("%s:%d", target.ServerIP, target.Port)
	r, rtt, err := c.Exchange(m, server)
	if err != nil {
		p.failed(result, target, now, err)
		return
	}

	// Check DNS response
	if r.Rcode != dns.RcodeSuccess {
		p.failed(result, target, now, fmt.Errorf("DNS error: %s", dns.RcodeToString[r.Rcode]))
		return
	}

	// Success
	p.success(result, target, now, rtt)
}

func (p *DNSProber) sent(result chan *Event, target *DNSTarget, sentTime time.Time) {
	displayName := fmt.Sprintf("%s/%s/%s", target.Server, target.Domain, target.RecordType)
	result <- &Event{
		Key:         target.Key,
		DisplayName: displayName,
		Result:      SENT,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     "",
	}
}

func (p *DNSProber) success(result chan *Event, target *DNSTarget, sentTime time.Time, rtt time.Duration) {
	displayName := fmt.Sprintf("%s/%s/%s", target.Server, target.Domain, target.RecordType)
	result <- &Event{
		Key:         target.Key,
		DisplayName: displayName,
		Result:      SUCCESS,
		SentTime:    sentTime,
		Rtt:         rtt,
		Message:     "",
	}
}

func (p *DNSProber) failed(result chan *Event, target *DNSTarget, sentTime time.Time, err error) {
	reason := FAILED
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		reason = TIMEOUT
	}

	displayName := fmt.Sprintf("%s/%s/%s", target.Server, target.Domain, target.RecordType)
	result <- &Event{
		Key:         target.Key,
		DisplayName: displayName,
		Result:      reason,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     err.Error(),
	}
}
