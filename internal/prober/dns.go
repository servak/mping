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
		targets  []string
		config   *DNSConfig
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
	}
)

func NewDNSProber(cfg *DNSConfig) *DNSProber {
	return &DNSProber{
		targets:  make([]string, 0),
		config:   cfg,
		exitChan: make(chan bool),
	}
}

// Accept parses DNS targets in format: dns://server[:port]/domain[/record_type]
func (p *DNSProber) Accept(target string) (ProbeTarget, error) {
	if !strings.HasPrefix(target, "dns://") {
		return ProbeTarget{}, ErrNotAccepted
	}

	dnsTarget, err := p.parseTarget(target)
	if err != nil {
		return ProbeTarget{}, fmt.Errorf("invalid DNS target: %w", err)
	}

	// Validate DNS server by resolving its IP
	serverIP, err := net.ResolveIPAddr("ip", dnsTarget.Server)
	if err != nil {
		return ProbeTarget{}, fmt.Errorf("failed to resolve DNS server '%s': %w", dnsTarget.Server, err)
	}

	// Store the formatted target string for probing and use as key
	targetStr := fmt.Sprintf("%s:%d|%s|%s|%t", serverIP.String(), dnsTarget.Port, dnsTarget.Domain, dnsTarget.RecordType, dnsTarget.UseTCP)
	displayName := fmt.Sprintf("%s/%s/%s", dnsTarget.Server, dnsTarget.Domain, dnsTarget.RecordType)

	p.targets = append(p.targets, targetStr)

	return ProbeTarget{
		Key:         targetStr, // Use same format as Event.Target
		DisplayName: displayName,
	}, nil
}

func (p *DNSProber) HasTargets() bool {
	return len(p.targets) > 0
}

func (p *DNSProber) parseTarget(target string) (*DNSTarget, error) {
	// Remove dns:// prefix
	target = strings.TrimPrefix(target, "dns://")
	
	// Split into server and query parts
	parts := strings.SplitN(target, "/", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid format, expected: dns://server/domain[/record_type]")
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

func (p *DNSProber) sendProbe(result chan *Event, target string, timeout time.Duration) {
	p.wg.Add(1)
	defer p.wg.Done()
	
	// Parse stored target format: server:port|domain|recordType|useTCP
	parts := strings.Split(target, "|")
	if len(parts) != 4 {
		p.failed(result, target, time.Now(), fmt.Errorf("invalid target format"))
		return
	}

	server := parts[0]
	domain := parts[1]
	recordType := parts[2]
	useTCP := parts[3] == "true"

	now := time.Now()
	p.sent(result, target, now)

	// Create DNS client
	c := new(dns.Client)
	c.Timeout = timeout
	if useTCP {
		c.Net = "tcp"
	}

	// Create DNS query
	m := new(dns.Msg)
	qtype := dns.StringToType[recordType]
	if qtype == 0 {
		p.failed(result, target, now, fmt.Errorf("unsupported record type: %s", recordType))
		return
	}
	
	m.SetQuestion(dns.Fqdn(domain), qtype)

	// Send DNS query
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

func (p *DNSProber) sent(result chan *Event, target string, sentTime time.Time) {
	result <- &Event{
		Target:   target,
		Result:   SENT,
		SentTime: sentTime,
		Rtt:      0,
		Message:  "",
	}
}

func (p *DNSProber) success(result chan *Event, target string, sentTime time.Time, rtt time.Duration) {
	result <- &Event{
		Target:   target,
		Result:   SUCCESS,
		SentTime: sentTime,
		Rtt:      rtt,
		Message:  "",
	}
}

func (p *DNSProber) failed(result chan *Event, target string, sentTime time.Time, err error) {
	reason := FAILED
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		reason = TIMEOUT
	}

	result <- &Event{
		Target:   target,
		Result:   reason,
		SentTime: sentTime,
		Rtt:      0,
		Message:  err.Error(),
	}
}