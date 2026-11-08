package prober

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	NTP ProbeType = "ntp"
)

type (
	// ntpTarget is one monitored NTP server.
	ntpTarget struct {
		display string // original target string, for display
		host    string // server host name or IP literal
		port    int
		literal bool // host is an IP literal and never needs resolving
	}

	NTPProber struct {
		targets map[string]*ntpTarget // "host:port" -> target
		config  *NTPConfig
		prefix  string
		loop    *runLoop
		// resolve looks up a host:port; replaceable in tests.
		resolve func(network, address string) (*net.UDPAddr, error)
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
		targets: make(map[string]*ntpTarget),
		config:  cfg,
		prefix:  prefix,
		loop:    newRunLoop(),
		resolve: net.ResolveUDPAddr,
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

	// Resolve once to reject unresolvable names early. Probes resolve again
	// (before T1) so that pool members that come and go are followed.
	serverAddr := net.JoinHostPort(server, strconv.Itoa(port))
	if _, err := p.resolve("udp", serverAddr); err != nil {
		return fmt.Errorf("failed to resolve NTP server '%s': %w", serverAddr, err)
	}

	// Store target with server:port as key for uniqueness
	_, litErr := netip.ParseAddr(server)
	p.targets[serverAddr] = &ntpTarget{display: target, host: server, port: port, literal: litErr == nil}

	return nil
}

func (p *NTPProber) parseTarget(target string) (string, int, error) {
	originalTarget := target

	target, _ = cutPrefix(target, p.prefix)

	// Parse server and port
	server := p.config.Server
	port := p.config.Port

	if target != "" {
		host, portStr, err := splitServerPort(target)
		if err != nil {
			return "", 0, err
		}
		server = host
		if portStr != "" {
			p, err := net.LookupPort("udp", portStr)
			if err != nil || p <= 0 || p > 65535 {
				return "", 0, fmt.Errorf("invalid port: %s", portStr)
			}
			port = p
		}
	}

	if server == "" {
		return "", 0, fmt.Errorf("NTP server is required in target: %s", originalTarget)
	}

	return server, port, nil
}

func (p *NTPProber) Start(result chan *Event, interval, timeout time.Duration) error {
	return runPeriodic(p.loop, result, NTP, interval, slices.Sorted(maps.Keys(p.targets)),
		func(serverAddr string) (string, string) { return serverAddr, p.targets[serverAddr].display },
		func(serverAddr string) { p.sendProbe(result, serverAddr, timeout) })
}

// Stop stops probing and waits for Start to return. It is safe to call more than once.
func (p *NTPProber) Stop() {
	p.loop.stop()
}

func (p *NTPProber) sendProbe(result chan *Event, serverAddr string, timeout time.Duration) {
	now := time.Now()
	tgt := p.targets[serverAddr]
	displayName := tgt.display
	result <- sentEvent(serverAddr, displayName, now)

	// Resolve on every probe, before T1 is taken, so DNS time stays out of the
	// measurement while a dead pool member does not stick for the whole
	// session. IP literals are used as they are.
	dest := serverAddr
	if !tgt.literal {
		udpAddr, err := p.resolve("udp", serverAddr)
		if err != nil {
			result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to resolve NTP server '%s': %w", serverAddr, err))
			return
		}
		dest = udpAddr.String()
	}

	conn, err := net.DialTimeout("udp", dest, timeout)
	if err != nil {
		result <- errorEvent(serverAddr, displayName, now, err)
		return
	}
	defer conn.Close()

	// Set deadline for the entire operation
	conn.SetDeadline(time.Now().Add(timeout))

	// Create and send NTP request; T1 is taken right before the write.
	req := &ntpPacket{
		Settings: 0x1B, // leap indicator=0, version=3, mode=3 (client)
	}
	t1 := time.Now()
	req.TxTimeSec, req.TxTimeFrac = ntpTimeFromTime(t1)

	var reqBuf bytes.Buffer
	_ = binary.Write(&reqBuf, binary.BigEndian, req)
	if _, err = conn.Write(reqBuf.Bytes()); err != nil {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to send NTP request: %w", err))
		return
	}

	// Read the response as a single datagram.
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	t4 := time.Now() // T4 is taken right after the read
	if err != nil {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to read NTP response: %w", err))
		return
	}
	if n < ntpPacketSize {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("short NTP response: %d bytes (need %d)", n, ntpPacketSize))
		return
	}
	var resp ntpPacket
	if err := binary.Read(bytes.NewReader(buf[:ntpPacketSize]), binary.BigEndian, &resp); err != nil {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("failed to parse NTP response: %w", err))
		return
	}

	if err := validateNTPResponse(&resp, req); err != nil {
		result <- errorEvent(serverAddr, displayName, now, err)
		return
	}

	// RFC 5905: offset = ((T2-T1)+(T3-T4))/2, delay = (T4-T1)-(T3-T2)
	t2 := ntpTimeToTime(resp.RxTimeSec, resp.RxTimeFrac)
	t3 := ntpTimeToTime(resp.TxTimeSec, resp.TxTimeFrac)
	if resp.RxTimeSec == 0 && resp.RxTimeFrac == 0 {
		t2 = t3 // server did not report a receive time; assume no processing delay
	}
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	rtt := t4.Sub(t1) - t3.Sub(t2)
	// Clamp to a small positive value: stats treats a zero RTT as "unset", so
	// a clock-skew artifact (negative delay) must not be reported as 0.
	if rtt < time.Microsecond {
		rtt = time.Microsecond
	}

	// Check if offset exceeds maximum allowed
	if p.config.MaxOffset > 0 && offset.Abs() > p.config.MaxOffset {
		result <- errorEvent(serverAddr, displayName, now, fmt.Errorf("time offset too large: %v (max: %v)", offset, p.config.MaxOffset))
		return
	}

	// Success
	result <- successEvent(serverAddr, displayName, now, rtt, ntpDetails(serverAddr, displayName, &resp, offset))
}

const ntpPacketSize = 48

// validateNTPResponse rejects responses that cannot be trusted as an answer to req.
func validateNTPResponse(resp, req *ntpPacket) error {
	if mode := resp.Settings & 0x07; mode != 4 {
		return fmt.Errorf("invalid NTP response: mode %d (expected 4, server)", mode)
	}
	// The origin timestamp is checked first: a packet that does not answer our
	// request (e.g. a spoofed or stale stratum-0 one) must not be reported as KoD.
	if resp.OrigTimeSec != req.TxTimeSec || resp.OrigTimeFrac != req.TxTimeFrac {
		return fmt.Errorf("invalid NTP response: origin timestamp does not match the request")
	}
	if resp.Stratum == 0 {
		code := strings.TrimRight(string([]byte{
			byte(resp.ReferenceID >> 24), byte(resp.ReferenceID >> 16),
			byte(resp.ReferenceID >> 8), byte(resp.ReferenceID),
		}), "\x00 ")
		return fmt.Errorf("NTP server sent Kiss-o'-Death (stratum 0), code %q", code)
	}
	if resp.Stratum >= 16 {
		return fmt.Errorf("NTP server is unsynchronized or invalid (stratum %d)", resp.Stratum)
	}
	if li := resp.Settings >> 6; li == 3 {
		return fmt.Errorf("NTP server is unsynchronized (leap indicator 3)")
	}
	if resp.TxTimeSec == 0 && resp.TxTimeFrac == 0 {
		return fmt.Errorf("invalid NTP response: transmit timestamp is zero")
	}
	return nil
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

	// NTP seconds wrap every 2^32 s (era 0 ends in Feb 2036). Values below
	// 2^31 would be dates before 1968, so they are taken as era 1.
	full := int64(sec)
	if sec < 1<<31 {
		full += 1 << 32
	}
	unix := full - ntpEpochOffset
	nsec := int64(frac) * 1000000000 / ntpFracScale

	return time.Unix(unix, nsec)
}
