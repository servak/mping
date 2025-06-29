package prober

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	TCP ProbeType = "tcp"
)

type (
	TCPProber struct {
		targets  []string
		config   *TCPConfig
		exitChan chan bool
		wg       sync.WaitGroup
	}

	TCPConfig struct {
		SourceInterface string `yaml:"source_interface"`
		Timeout         string `yaml:"timeout"`
	}
)

func NewTCPProber(cfg *TCPConfig) *TCPProber {
	return &TCPProber{
		targets:  make([]string, 0),
		config:   cfg,
		exitChan: make(chan bool),
	}
}

func (p *TCPProber) Accept(target string) (string, error) {
	if !strings.HasPrefix(target, "tcp://") {
		return "", ErrNotAccepted
	}
	
	host, port, err := p.parseTarget(target)
	if err != nil {
		return "", fmt.Errorf("invalid TCP target: %w", err)
	}
	
	p.targets = append(p.targets, target)
	return net.JoinHostPort(host, port), nil
}

func (p *TCPProber) HasTargets() bool {
	return len(p.targets) > 0
}

func (p *TCPProber) Start(result chan *Event, interval, timeout time.Duration) error {
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

func (p *TCPProber) Stop() {
	close(p.exitChan)
	p.wg.Wait()
}


func (p *TCPProber) sendProbe(result chan *Event, target string, timeout time.Duration) {
	// Parse target to extract host and port
	host, port, err := p.parseTarget(target)
	if err != nil {
		p.failed(result, target, time.Now(), err)
		return
	}

	// Use host:port format for display
	displayTarget := net.JoinHostPort(host, port)
	
	now := time.Now()
	p.sent(result, displayTarget, now)

	// Create dialer with timeout
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	// Set source interface if specified
	if p.config.SourceInterface != "" {
		if localAddr, err := p.getSourceAddr(p.config.SourceInterface); err == nil {
			dialer.LocalAddr = localAddr
		}
	}

	// Attempt TCP connection
	conn, err := dialer.Dial("tcp", net.JoinHostPort(host, port))
	rtt := time.Since(now)

	if err != nil {
		p.failed(result, displayTarget, now, err)
		return
	}

	// Close connection immediately after successful establishment
	conn.Close()
	p.success(result, displayTarget, now, rtt)
}

func (p *TCPProber) parseTarget(target string) (host, port string, err error) {
	// Remove tcp:// prefix if present
	target = strings.TrimPrefix(target, "tcp://")
	
	// Parse host:port
	host, port, err = net.SplitHostPort(target)
	if err != nil {
		return "", "", fmt.Errorf("invalid target format: %s", target)
	}
	
	return host, port, nil
}

func (p *TCPProber) getSourceAddr(interfaceName string) (net.Addr, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return &net.TCPAddr{IP: ipnet.IP}, nil
			}
		}
	}

	return nil, fmt.Errorf("no suitable address found on interface %s", interfaceName)
}

func (p *TCPProber) sent(result chan *Event, target string, sentTime time.Time) {
	result <- &Event{
		Target:   target,
		Result:   SENT,
		SentTime: sentTime,
		Rtt:      0,
		Message:  "",
	}
}

func (p *TCPProber) success(result chan *Event, target string, sentTime time.Time, rtt time.Duration) {
	result <- &Event{
		Target:   target,
		Result:   SUCCESS,
		SentTime: sentTime,
		Rtt:      rtt,
		Message:  "",
	}
}

func (p *TCPProber) failed(result chan *Event, target string, sentTime time.Time, err error) {
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