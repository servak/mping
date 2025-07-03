package prober

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	NTP ProbeType = "ntp"
)

type (
	NTPProber struct {
		targets  map[string]string // server address -> display name
		config   *NTPConfig
		prefix   string
		exitChan chan bool
		wg       sync.WaitGroup
	}

	NTPConfig struct {
		Server    string        `yaml:"server"`
		Port      int           `yaml:"port,omitempty"`
		MaxOffset time.Duration `yaml:"max_offset,omitempty"` // Alert if offset > this value
	}

	// NTP packet structure (simplified)
	ntpPacket struct {
		Settings       uint8  // leap year indicator, version number, and mode
		Stratum        uint8  // stratum level of the local clock
		Poll           int8   // maximum interval between successive messages
		Precision      int8   // precision of the local clock
		RootDelay      uint32 // total round trip delay time
		RootDispersion uint32 // max error aloud from primary clock source
		ReferenceID    uint32 // reference clock identifier
		RefTimeSec     uint32 // reference time-stamp seconds
		RefTimeFrac    uint32 // reference time-stamp fraction of a second
		OrigTimeSec    uint32 // origin time-stamp seconds
		OrigTimeFrac   uint32 // origin time-stamp fraction of a second
		RxTimeSec      uint32 // receive time-stamp seconds
		RxTimeFrac     uint32 // receive time-stamp fraction of a second
		TxTimeSec      uint32 // transmit time-stamp seconds
		TxTimeFrac     uint32 // transmit time-stamp fraction of a second
	}
)

// Validate validates the NTP configuration
func (cfg *NTPConfig) Validate() error {
	if cfg.Server == "" {
		return fmt.Errorf("NTP server is required")
	}
	
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid NTP server port: %d (must be 1-65535)", cfg.Port)
	}
	
	return nil
}

func NewNTPProber(cfg *NTPConfig, prefix string) *NTPProber {
	return &NTPProber{
		targets:  make(map[string]string),
		config:   cfg,
		prefix:   prefix,
		exitChan: make(chan bool),
	}
}

// Accept parses NTP targets in format: ntp://server[:port] or ntp:server[:port]
func (p *NTPProber) Accept(target string) error {
	// Check if it's new format (ntp://...) or legacy format (ntp:...)
	if !strings.HasPrefix(target, p.prefix+"://") && !strings.HasPrefix(target, p.prefix+":") {
		return ErrNotAccepted
	}

	server, port, err := p.parseTarget(target)
	if err != nil {
		return fmt.Errorf("invalid NTP target: %w", err)
	}

	// Validate NTP server by resolving its address
	_, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", server, port))
	if err != nil {
		return fmt.Errorf("failed to resolve NTP server '%s:%d': %w", server, port, err)
	}

	// Store target with server:port as key for uniqueness
	serverAddr := fmt.Sprintf("%s:%d", server, port)
	p.targets[serverAddr] = target

	return nil
}

func (p *NTPProber) parseTarget(target string) (string, int, error) {
	originalTarget := target
	
	// Remove ntp:// or ntp: prefix
	if strings.HasPrefix(target, p.prefix+"://") {
		target = strings.TrimPrefix(target, p.prefix+"://")
	} else if strings.HasPrefix(target, p.prefix+":") {
		target = strings.TrimPrefix(target, p.prefix+":")
	}

	// Parse server and port
	server := p.config.Server
	port := p.config.Port
	
	if target != "" {
		if strings.Contains(target, ":") {
			host, portStr, err := net.SplitHostPort(target)
			if err != nil {
				return "", 0, fmt.Errorf("invalid server:port format: %w", err)
			}
			server = host
			if p, err := net.LookupPort("udp", portStr); err == nil {
				port = p
			} else {
				return "", 0, fmt.Errorf("invalid port: %s", portStr)
			}
		} else {
			server = target
		}
	}

	if server == "" {
		return "", 0, fmt.Errorf("NTP server is required in target: %s", originalTarget)
	}

	return server, port, nil
}

func (p *NTPProber) emitRegistrationEvents(r chan *Event) {
	for serverAddr, displayName := range p.targets {
		r <- &Event{
			Key:         serverAddr,
			DisplayName: displayName,
			Result:      REGISTER,
		}
	}
}

func (p *NTPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	p.emitRegistrationEvents(result)
	ticker := time.NewTicker(interval)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for serverAddr := range p.targets {
			go p.sendProbe(result, serverAddr, timeout)
		}
		for {
			select {
			case <-p.exitChan:
				ticker.Stop()
				return
			case <-ticker.C:
				for serverAddr := range p.targets {
					go p.sendProbe(result, serverAddr, timeout)
				}
			}
		}
	}()
	p.wg.Wait()
	return nil
}

func (p *NTPProber) Stop() {
	close(p.exitChan)
	p.wg.Wait()
}

func (p *NTPProber) sendProbe(result chan *Event, serverAddr string, timeout time.Duration) {
	p.wg.Add(1)
	defer p.wg.Done()

	now := time.Now()
	displayName := p.targets[serverAddr]
	p.sent(result, serverAddr, displayName, now)

	// Parse server address
	host, portStr, err := net.SplitHostPort(serverAddr)
	if err != nil {
		p.failed(result, serverAddr, displayName, now, fmt.Errorf("invalid server address: %w", err))
		return
	}

	// Connect to NTP server
	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, portStr), timeout)
	if err != nil {
		p.failed(result, serverAddr, displayName, now, err)
		return
	}
	defer conn.Close()

	// Set deadline for the entire operation
	conn.SetDeadline(time.Now().Add(timeout))

	// Create and send NTP request
	req := &ntpPacket{
		Settings: 0x1B, // leap indicator=0, version=3, mode=3 (client)
	}

	// Set transmit timestamp
	req.TxTimeSec, req.TxTimeFrac = ntpTimeFromTime(now)

	err = binary.Write(conn, binary.BigEndian, req)
	if err != nil {
		p.failed(result, serverAddr, displayName, now, fmt.Errorf("failed to send NTP request: %w", err))
		return
	}

	// Read response
	var resp ntpPacket
	err = binary.Read(conn, binary.BigEndian, &resp)
	if err != nil {
		p.failed(result, serverAddr, displayName, now, fmt.Errorf("failed to read NTP response: %w", err))
		return
	}

	// Calculate RTT and offset
	rtt := time.Since(now)
	
	// Convert NTP timestamp to time.Time
	serverTime := ntpTimeToTime(resp.TxTimeSec, resp.TxTimeFrac)
	offset := serverTime.Sub(now.Add(rtt / 2))

	// Check if offset exceeds maximum allowed
	if p.config.MaxOffset > 0 && offset.Abs() > p.config.MaxOffset {
		p.failed(result, serverAddr, displayName, now, fmt.Errorf("time offset too large: %v (max: %v)", offset, p.config.MaxOffset))
		return
	}

	// Success
	p.success(result, serverAddr, displayName, now, rtt)
}

func (p *NTPProber) sent(result chan *Event, serverAddr, displayName string, sentTime time.Time) {
	result <- &Event{
		Key:         serverAddr,
		DisplayName: displayName,
		Result:      SENT,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     "",
	}
}

func (p *NTPProber) success(result chan *Event, serverAddr, displayName string, sentTime time.Time, rtt time.Duration) {
	result <- &Event{
		Key:         serverAddr,
		DisplayName: displayName,
		Result:      SUCCESS,
		SentTime:    sentTime,
		Rtt:         rtt,
		Message:     "",
	}
}

func (p *NTPProber) failed(result chan *Event, serverAddr, displayName string, sentTime time.Time, err error) {
	reason := FAILED
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		reason = TIMEOUT
	}
	result <- &Event{
		Key:         serverAddr,
		DisplayName: displayName,
		Result:      reason,
		SentTime:    sentTime,
		Rtt:         0,
		Message:     err.Error(),
	}
}

// ntpTimeFromTime converts time.Time to NTP timestamp format
func ntpTimeFromTime(t time.Time) (uint32, uint32) {
	// NTP epoch is January 1, 1900, Unix epoch is January 1, 1970
	const ntpEpochOffset = 2208988800 // seconds between 1900 and 1970
	
	unix := t.Unix()
	sec := uint32(unix + ntpEpochOffset)
	frac := uint32(t.Nanosecond() * 4294967296 / 1000000000) // Convert nanoseconds to NTP fraction
	
	return sec, frac
}

// ntpTimeToTime converts NTP timestamp to time.Time
func ntpTimeToTime(sec, frac uint32) time.Time {
	const ntpEpochOffset = 2208988800 // seconds between 1900 and 1970
	
	unix := int64(sec) - ntpEpochOffset
	nsec := int64(frac) * 1000000000 / 4294967296
	
	return time.Unix(unix, nsec)
}