package prober

import (
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"time"
)

const (
	NTP ProbeType = "ntp"
)

type (
	NTPProber struct {
		targets map[string]string // server address -> display name
		config  *NTPConfig
		prefix  string
		loop    *runLoop
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
		targets: make(map[string]string),
		config:  cfg,
		prefix:  prefix,
		loop:    newRunLoop(),
	}
}

// Accept parses NTP targets in format: ntp://server[:port] or ntp:server[:port]
func (p *NTPProber) Accept(target string) error {
	if _, ok := cutPrefix(target, p.prefix); !ok {
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

	target, _ = cutPrefix(target, p.prefix)

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

func (p *NTPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	return runPeriodic(p.loop, result, interval, slices.Sorted(maps.Keys(p.targets)),
		func(serverAddr string) (string, string) { return serverAddr, p.targets[serverAddr] },
		func(serverAddr string) { p.sendProbe(result, serverAddr, timeout) })
}

// Stop stops probing and waits for Start to return. It is safe to call more than once.
func (p *NTPProber) Stop() {
	p.loop.stop()
}

func (p *NTPProber) sendProbe(result chan *Event, serverAddr string, timeout time.Duration) {
	now := time.Now()
	displayName := p.targets[serverAddr]
	result <- sentEvent(serverAddr, displayName, now)

	// Parse server address
	host, portStr, err := net.SplitHostPort(serverAddr)
	if err != nil {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("invalid server address: %w", err))
		return
	}

	// Connect to NTP server
	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, portStr), timeout)
	if err != nil {
		result <- errorEvent(serverAddr, displayName, now, err)
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
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to send NTP request: %w", err))
		return
	}

	// Read response
	var resp ntpPacket
	err = binary.Read(conn, binary.BigEndian, &resp)
	if err != nil {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to read NTP response: %w", err))
		return
	}

	// Calculate RTT and offset
	rtt := time.Since(now)

	// Convert NTP timestamp to time.Time
	serverTime := ntpTimeToTime(resp.TxTimeSec, resp.TxTimeFrac)
	offset := serverTime.Sub(now.Add(rtt / 2))

	// Check if offset exceeds maximum allowed
	if p.config.MaxOffset > 0 && offset.Abs() > p.config.MaxOffset {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("time offset too large: %v (max: %v)", offset, p.config.MaxOffset))
		return
	}

	// Success
	result <- successEvent(serverAddr, displayName, now, rtt, ntpDetails(serverAddr, displayName, &resp, offset))
}

// ntpDetails describes a successful NTP response.
func ntpDetails(serverAddr, displayName string, resp *ntpPacket, offset time.Duration) *ProbeDetails {
	// Extract port (serverAddr is in "host:port" format)
	_, portStr, _ := net.SplitHostPort(serverAddr)
	port := 123 // Default NTP port
	if portStr != "" {
		if p, err := net.LookupPort("udp", portStr); err == nil {
			port = p
		}
	}

	return &ProbeDetails{
		ProbeType: "ntp",
		NTP: &NTPDetails{
			Server:    displayName,
			Port:      port,
			Stratum:   int(resp.Stratum),
			Offset:    offset.Microseconds(),
			Precision: int(resp.Precision),
		},
	}
}

// ntpTimeFromTime converts time.Time to NTP timestamp format
func ntpTimeFromTime(t time.Time) (uint32, uint32) {
	// NTP epoch is January 1, 1900, Unix epoch is January 1, 1970
	const ntpEpochOffset = 2208988800 // seconds between 1900 and 1970
	const ntpFracScale = 1 << 32      // 2^32 for NTP fraction conversion

	unix := t.Unix()
	sec := uint32(unix + ntpEpochOffset)
	frac := uint32(int64(t.Nanosecond()) * ntpFracScale / 1000000000) // Convert nanoseconds to NTP fraction

	return sec, frac
}

// ntpTimeToTime converts NTP timestamp to time.Time
func ntpTimeToTime(sec, frac uint32) time.Time {
	const ntpEpochOffset = 2208988800 // seconds between 1900 and 1970
	const ntpFracScale = 1 << 32      // 2^32 for NTP fraction conversion

	unix := int64(sec) - ntpEpochOffset
	nsec := int64(frac) * 1000000000 / ntpFracScale

	return time.Unix(unix, nsec)
}
