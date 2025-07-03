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
		targets  map[string]string // key (ip:port) -> displayName (host:port)
		config   *TCPConfig
		prefix   string
		exitChan chan bool
		wg       sync.WaitGroup
	}

	TCPConfig struct {
		SourceInterface string `yaml:"source_interface,omitempty"`
	}
)

func NewTCPProber(cfg *TCPConfig, prefix string) *TCPProber {
	return &TCPProber{
		targets:  make(map[string]string),
		config:   cfg,
		prefix:   prefix,
		exitChan: make(chan bool),
	}
}

func (p *TCPProber) Accept(target string) error {
	// Check if it's new format (tcp://host:port)
	if !strings.HasPrefix(target, p.prefix+"://") && !strings.HasPrefix(target, p.prefix+":") {
		return ErrNotAccepted
	}

	host, port, err := p.parseTarget(target)
	if err != nil {
		return fmt.Errorf("invalid TCP target: %w", err)
	}

	// DNS解決を事前に実行
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve '%s': %w", host, err)
	}

	// 最初のIPを使用
	ip := ips[0]
	ipPort := net.JoinHostPort(ip.String(), port)

	p.targets[ipPort] = target // key -> displayName mapping

	return nil
}

func (p *TCPProber) emitRegistrationEvents(r chan *Event) {
	for k, v := range p.targets {
		r <- &Event{
			Key:         k,
			DisplayName: v,
			Result:      REGISTER,
		}
	}
}

func (p *TCPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	p.emitRegistrationEvents(result)
	ticker := time.NewTicker(interval)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for target := range p.targets {
			go p.sendProbe(result, target, timeout)
		}
		for {
			select {
			case <-p.exitChan:
				ticker.Stop()
				return
			case <-ticker.C:
				for target := range p.targets {
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
	// target is already in "ip:port" format from Accept method
	now := time.Now()
	p.sent(result, target, now)

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

	// Attempt TCP connection using pre-resolved IP:port
	conn, err := dialer.Dial("tcp", target)
	rtt := time.Since(now)

	if err != nil {
		p.failed(result, target, now, err)
		return
	}

	// Close connection immediately after successful establishment
	conn.Close()
	p.success(result, target, now, rtt)
}

func (p *TCPProber) parseTarget(target string) (host, port string, err error) {
	// Remove tcp:// or tcp: prefix
	if strings.HasPrefix(target, p.prefix+"://") {
		target = strings.TrimPrefix(target, p.prefix+"://")
	} else if strings.HasPrefix(target, p.prefix+":") {
		target = strings.TrimPrefix(target, p.prefix+":")
	}

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
	displayName := p.targets[target] // Get displayName from targets map
	result <- &Event{
		Key:         target,
		DisplayName: displayName,
		Result:      SENT,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     "",
	}
}

func (p *TCPProber) success(result chan *Event, target string, sentTime time.Time, rtt time.Duration) {
	displayName := p.targets[target] // Get displayName from targets map
	result <- &Event{
		Key:         target,
		DisplayName: displayName,
		Result:      SUCCESS,
		SentTime:    sentTime,
		Rtt:         rtt,
		Message:     "",
	}
}

func (p *TCPProber) failed(result chan *Event, target string, sentTime time.Time, err error) {
	reason := FAILED
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		reason = TIMEOUT
	}

	displayName := p.targets[target] // Get displayName from targets map
	result <- &Event{
		Key:         target,
		DisplayName: displayName,
		Result:      reason,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     err.Error(),
	}
}
