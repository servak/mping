package prober

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	HTTP  ProbeType = "http"
	HTTPS ProbeType = "https"
)

type (
	HTTPProber struct {
		client   *http.Client
		targets  []string
		config   *HTTPConfig
		prefix   string // Custom prefix like "my-http", "http", "https", etc.
		exitChan chan bool
		wg       sync.WaitGroup
	}

	HTTPConfig struct {
		Header      http.Header `yaml:"headers,omitempty"`
		ExpectCode  int         `yaml:"expect_code,omitempty"`     // Single code (backward compatibility)
		ExpectCodes string      `yaml:"expect_codes,omitempty"`   // Range/list: "2XX", "200,201,202"
		ExpectBody  string      `yaml:"expect_body,omitempty"`
		TLS         *TLSConfig  `yaml:"tls,omitempty"`
		RedirectOFF bool        `yaml:"redirect_off,omitempty"`
	}

	TLSConfig struct {
		SkipVerify bool `yaml:"skip_verify"`
	}

	customTransport struct {
		transport http.RoundTripper
		headers   http.Header
	}
)

func NewHTTPProber(cfg *HTTPConfig, prefix string) *HTTPProber {
	var rd func(req *http.Request, via []*http.Request) error
	if cfg.RedirectOFF {
		rd = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// Determine TLS skip verification setting
	skipVerify := true
	if cfg.TLS != nil {
		skipVerify = cfg.TLS.SkipVerify // Use new TLS config if available
	}

	client := &http.Client{
		Transport: &customTransport{
			transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
			},
			headers: cfg.Header,
		},
		CheckRedirect: rd,
	}
	return &HTTPProber{
		client:   client,
		targets:  make([]string, 0),
		config:   cfg,
		prefix:   prefix,
		exitChan: make(chan bool),
	}
}

func (p *HTTPProber) Accept(target string) error {
	// Check if it matches our prefix (e.g., "my-http://host", "http://host")
	if !strings.HasPrefix(target, p.prefix+"://") {
		return ErrNotAccepted
	}

	// Extract the actual URL part
	hostname := strings.TrimPrefix(target, p.prefix+"://")

	// Create the actual HTTP URL for validation
	var actualURL string
	if p.config != nil && p.config.TLS != nil {
		actualURL = "https://" + hostname
	} else {
		actualURL = "http://" + hostname
	}

	// Validate URL format
	u, err := url.Parse(actualURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid HTTP URL format")
	}
	if slices.Contains(p.targets, target) {
		// Target already exists, no need to add it again
		return nil
	}
	p.targets = append(p.targets, target) // Store original target
	return nil

}

// convertToActualURL converts custom target to actual HTTP URL
func (p *HTTPProber) convertToActualURL(target string) string {
	// Extract hostname from custom target
	hostname := strings.TrimPrefix(target, p.prefix+"://")

	// Determine protocol based on TLS configuration
	if p.config != nil && p.config.TLS != nil {
		return "https://" + hostname
	}
	return "http://" + hostname
}

func (p *HTTPProber) sent(r chan *Event, t string) {
	r <- &Event{
		Key:         t,
		DisplayName: t,
		Result:      SENT,
	}
}

func (p *HTTPProber) timeout(r chan *Event, target string, now time.Time, err error) {
	r <- &Event{
		Key:         target,
		DisplayName: target,
		Result:      TIMEOUT,
		SentTime:    now,
		Rtt:         time.Since(now),
		Message:     "timeout",
	}
}

func (p *HTTPProber) failed(r chan *Event, target string, now time.Time, err error) {
	r <- &Event{
		Key:         target,
		DisplayName: target,
		Result:      FAILED,
		SentTime:    now,
		Rtt:         time.Since(now),
		Message:     err.Error(),
	}
}

func (p *HTTPProber) probe(r chan *Event, target string) {
	p.wg.Add(1)
	defer p.wg.Done()
	now := time.Now()
	p.sent(r, target)

	// Convert target to actual HTTP URL
	actualURL := p.convertToActualURL(target)
	resp, err := p.client.Get(actualURL)
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Timeout() {
			p.timeout(r, target, now, err)
		} else {
			p.failed(r, target, now, err)
		}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.failed(r, target, now, err)
		return
	}
	if !p.isExpectedStatusCode(resp.StatusCode) {
		p.failed(r, target, now, fmt.Errorf("unexpected status code: %d", resp.StatusCode))
	} else if p.config.ExpectBody != "" && p.config.ExpectBody != strings.TrimRight(string(body), "\n") {
		p.failed(r, target, now, errors.New("invalid body"))
	} else {
		r <- &Event{
			Key:         target,
			DisplayName: target,
			Result:      SUCCESS,
			SentTime:    now,
			Rtt:         time.Since(now),
		}
	}
}

func (p *HTTPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	p.client.Timeout = timeout
	ticker := time.NewTicker(interval)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for _, target := range p.targets {
			go p.probe(r, target)
		}
		for {
			select {
			case <-p.exitChan:
				return
			case <-ticker.C:
				for _, target := range p.targets {
					go p.probe(r, target)
				}
			}
		}
	}()
	p.wg.Wait()
	return nil
}

func (p *HTTPProber) Stop() {
	p.exitChan <- true
}

func (c *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range c.headers {
		req.Header[k] = v
	}
	return c.transport.RoundTrip(req)
}

// isExpectedStatusCode checks if the given status code matches the expected criteria
func (p *HTTPProber) isExpectedStatusCode(statusCode int) bool {
	// If ExpectCodes is specified, use it; otherwise fall back to ExpectCode
	if p.config.ExpectCodes != "" {
		return MatchCode(statusCode, p.config.ExpectCodes)
	}
	
	// Backward compatibility: use ExpectCode (default 0 means any code is ok)
	if p.config.ExpectCode == 0 {
		return true // No specific code expected
	}
	return statusCode == p.config.ExpectCode
}
