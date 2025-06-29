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
	TCPTarget struct {
		Original    string
		Host        string 
		Port        string
		DisplayName string
	}
	
	TCPProber struct {
		targets  []*TCPTarget
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
		targets:  make([]*TCPTarget, 0),
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
	
	displayName := net.JoinHostPort(host, port)
	tcpTarget := &TCPTarget{
		Original:    target,
		Host:        host,
		Port:        port,
		DisplayName: displayName,
	}
	
	p.targets = append(p.targets, tcpTarget)
	return displayName, nil
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
					go p.sendProbeTarget(result, target, timeout)
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


func (p *TCPProber) sendProbeTarget(result chan *Event, target *TCPTarget, timeout time.Duration) {
	now := time.Now()
	p.sent(result, target.DisplayName, now)

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
	conn, err := dialer.Dial("tcp", net.JoinHostPort(target.Host, target.Port))
	rtt := time.Since(now)

	if err != nil {
		p.failed(result, target.DisplayName, now, err)
		return
	}

	// Close connection immediately after successful establishment
	conn.Close()
	p.success(result, target.DisplayName, now, rtt)
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