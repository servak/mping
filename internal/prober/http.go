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
		client    *http.Client
		targets   []string
		config    *HTTPConfig
		probeType ProbeType // HTTP or HTTPS (the "probe:" value of the prober config)
		prefix    string    // Custom prefix like "my-http", "http", "https", etc.
		exitChan  chan bool
		wg        sync.WaitGroup
	}

	HTTPConfig struct {
		Header      map[string]string `yaml:"headers,omitempty"`
		ExpectCodes string            `yaml:"expect_codes"` // Range/list: "200,201,202", "200-299"
		ExpectBody  string            `yaml:"expect_body,omitempty"`
		TLS         *TLSConfig        `yaml:"tls,omitempty"`
		RedirectOFF bool              `yaml:"redirect_off,omitempty"`
	}

	TLSConfig struct {
		// SkipVerify controls TLS certificate verification.
		// nil (unset) means the default, which is to skip verification.
		SkipVerify *bool `yaml:"skip_verify,omitempty"`
	}

	customTransport struct {
		transport http.RoundTripper
		headers   map[string]string
	}
)

// ShouldSkipVerify reports whether TLS certificate verification should be skipped.
// A nil TLSConfig or an unset skip_verify falls back to the default (true).
func (t *TLSConfig) ShouldSkipVerify() bool {
	if t == nil || t.SkipVerify == nil {
		return true
	}
	return *t.SkipVerify
}

// BoolPtr returns a pointer to b. Useful for optional boolean config fields.
func BoolPtr(b bool) *bool {
	return &b
}

// NewHTTPProber creates an HTTP(S) prober. probeType is the "probe:" value of the
// prober config (HTTP or HTTPS) and is used to pick the default URL scheme for
// custom prefixes (see resolveURL).
func NewHTTPProber(probeType ProbeType, cfg *HTTPConfig, prefix string) *HTTPProber {
	var rd func(req *http.Request, via []*http.Request) error
	if cfg.RedirectOFF {
		rd = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// Determine TLS skip verification setting (default: skip)
	skipVerify := cfg.TLS.ShouldSkipVerify()

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
		client:    client,
		targets:   make([]string, 0),
		config:    cfg,
		probeType: probeType,
		prefix:    prefix,
		exitChan:  make(chan bool),
	}
}

func (p *HTTPProber) Accept(target string) error {
	// Check if it matches our prefix (e.g., "my-http://host", "http://host")
	if !strings.HasPrefix(target, p.prefix+"://") {
		return ErrNotAccepted
	}

	scheme, rest := p.resolveURL(target)

	// The remainder must be a plain "host[:port][/path]". Anything still
	// containing "://" (e.g. "http://ftp://host") is malformed.
	if rest == "" || strings.Contains(rest, "://") {
		return fmt.Errorf("invalid HTTP URL format")
	}

	// Validate URL format
	u, err := url.Parse(scheme + "://" + rest)
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

// resolveURL splits a target ("<prefix>://<rest>") into the URL scheme to use
// and the remainder (host[:port][/path]). The scheme is decided as follows:
//
//  1. Built-in prefixes: "https" always uses https and "http" always uses http,
//     regardless of the probe type or tls block.
//  2. Custom prefixes whose remainder carries an explicit scheme
//     ("api-check://https://host") use that scheme; it is stripped from rest.
//  3. Otherwise the prober config decides: "probe: https" -> https; a tls block
//     present -> https (backward compatibility with older configs); else http.
func (p *HTTPProber) resolveURL(target string) (scheme, rest string) {
	rest = strings.TrimPrefix(target, p.prefix+"://")

	switch p.prefix {
	case string(HTTPS):
		return "https", rest
	case string(HTTP):
		return "http", rest
	}

	for _, s := range []string{"https", "http"} {
		if after, ok := strings.CutPrefix(rest, s+"://"); ok {
			return s, after
		}
	}

	if p.probeType == HTTPS || (p.config != nil && p.config.TLS != nil) {
		return "https", rest
	}
	return "http", rest
}

// convertToActualURL converts custom target to actual HTTP URL
func (p *HTTPProber) convertToActualURL(target string) string {
	scheme, rest := p.resolveURL(target)
	return scheme + "://" + rest
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
	defer p.wg.Done()
	now := time.Now()
	p.sent(r, target)

	// Convert target to actual HTTP URL
	scheme, rest := p.resolveURL(target)
	actualURL := scheme + "://" + rest
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
		// Create HTTP detail information
		headers := make(map[string]string)
		for key, values := range resp.Header {
			if len(values) > 0 {
				headers[key] = values[0] // Get only the first value
			}
		}

		// Compare against the URL actually requested (not the target, which
		// may carry a custom prefix) to detect redirects.
		var redirects []string
		if finalURL := resp.Request.URL.String(); finalURL != actualURL {
			redirects = append(redirects, finalURL)
		}

		details := &ProbeDetails{
			ProbeType: scheme,
			HTTP: &HTTPDetails{
				StatusCode:   resp.StatusCode,
				ResponseSize: int64(len(body)),
				Headers:      headers,
				Redirects:    redirects,
			},
		}

		r <- &Event{
			Key:         target,
			DisplayName: target,
			Result:      SUCCESS,
			SentTime:    now,
			Rtt:         time.Since(now),
			Details:     details,
		}
	}
}

func (p *HTTPProber) emitRegistrationEvents(r chan *Event) {
	for _, v := range p.targets {
		r <- &Event{
			Key:         v,
			DisplayName: v,
			Result:      REGISTER,
		}
	}
}

func (p *HTTPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	p.emitRegistrationEvents(r)
	p.client.Timeout = timeout
	ticker := time.NewTicker(interval)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for _, target := range p.targets {
			p.wg.Add(1)
			go p.probe(r, target)
		}
		for {
			select {
			case <-p.exitChan:
				return
			case <-ticker.C:
				for _, target := range p.targets {
					p.wg.Add(1)
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
	if len(c.headers) == 0 {
		return c.transport.RoundTrip(req)
	}
	// A RoundTripper must not modify the caller's request.
	req = req.Clone(req.Context())
	for k, v := range c.headers {
		// net/http ignores Header["Host"]; the Host header is taken from req.Host.
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	return c.transport.RoundTrip(req)
}

// Validate validates the HTTP configuration
func (cfg *HTTPConfig) Validate() error {
	if cfg.ExpectCodes != "" {
		if !IsValidCodePattern(cfg.ExpectCodes) {
			return fmt.Errorf("invalid expect_codes pattern: %s", cfg.ExpectCodes)
		}
	}
	return nil
}

// isExpectedStatusCode checks if the given status code matches the expected criteria
func (p *HTTPProber) isExpectedStatusCode(statusCode int) bool {
	// If ExpectCodes is specified, use it; otherwise any status code is accepted
	if p.config.ExpectCodes != "" {
		return MatchCode(statusCode, p.config.ExpectCodes)
	}

	// Default: accept any status code when no expectation is configured
	return true
}
