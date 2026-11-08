package prober

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	HTTP  ProbeType = "http"
	HTTPS ProbeType = "https"

	// maxBodyBytes caps how much of a response body is read. It bounds memory
	// for huge or endless bodies; expect_body can only match within this size.
	maxBodyBytes = 1 << 20

	// maxCountBytes bounds how much of a body is read (and discarded) to
	// report its size when Content-Length is unknown. Larger bodies are
	// reported as maxCountBytes with SizeTruncated set.
	maxCountBytes = 64 << 20
)

type (
	HTTPProber struct {
		client    *http.Client
		targets   []string
		config    *HTTPConfig
		probeType ProbeType // HTTP or HTTPS (the "probe:" value of the prober config)
		prefix    string    // Custom prefix like "my-http", "http", "https", etc.
		loop      *runLoop
	}

	HTTPConfig struct {
		Header      map[string]string `yaml:"headers,omitempty"`
		ExpectCodes string            `yaml:"expect_codes"` // Range/list: "200,201,202", "200-299"
		ExpectBody  string            `yaml:"expect_body,omitempty"`
		TLS         *TLSConfig        `yaml:"tls,omitempty"`
		RedirectOFF bool              `yaml:"redirect_off,omitempty"`
		// KeepAlive reuses connections between probes. By default every probe
		// opens a fresh connection so the RTT always includes DNS, TCP and TLS
		// setup; with keep_alive only the first probe pays for them.
		// No omitempty: the setting stays visible in `config print` / `config init`.
		KeepAlive bool `yaml:"keep_alive"`
	}

	TLSConfig struct {
		// SkipVerify controls TLS certificate verification.
		// nil (unset) means the default, which is to skip verification.
		SkipVerify *bool `yaml:"skip_verify,omitempty"`
	}

	// customTransport adds the configured headers. The Host header is handled
	// specially: it (and the TLS server name, via hostTransport) applies only
	// to requests for the host the probe started with, never to a redirect
	// target on another host.
	customTransport struct {
		transport     http.RoundTripper // no Host override: SNI follows the URL host
		hostTransport http.RoundTripper // ServerName = host override
		headers       map[string]string
		host          string // resolved Host override; "" if none
	}

	// origHostKey carries the host of the originally requested URL in the
	// request context (redirected requests inherit it).
	origHostKey struct{}
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

	host := hostOverride(cfg.Header)
	newTransport := func(serverName string) *http.Transport {
		return &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: skipVerify, ServerName: serverName},
			DisableKeepAlives: !cfg.KeepAlive,
		}
	}
	ct := &customTransport{transport: newTransport(""), headers: cfg.Header, host: host}
	if host != "" {
		// With a Host header override, SNI and certificate verification use
		// that name instead of the URL host.
		name := host
		if h, _, err := net.SplitHostPort(name); err == nil {
			name = h
		}
		ct.hostTransport = newTransport(strings.Trim(name, "[]"))
	}
	client := &http.Client{Transport: ct, CheckRedirect: rd}
	return &HTTPProber{
		client:    client,
		targets:   make([]string, 0),
		config:    cfg,
		probeType: probeType,
		prefix:    prefix,
		loop:      newRunLoop(),
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

func (p *HTTPProber) probe(r chan *Event, target string) {
	now := time.Now()
	r <- sentEvent(target, target, now)
	fail := func(err error) {
		r <- failureEvent(target, target, FAILED, now, time.Since(now), err.Error())
	}

	// Convert target to actual HTTP URL
	scheme, rest := p.resolveURL(target)
	actualURL := scheme + "://" + rest
	failErr := func(err error) {
		if classifyErr(err) == TIMEOUT {
			r <- failureEvent(target, target, TIMEOUT, now, time.Since(now), "timeout")
		} else {
			fail(err)
		}
	}
	req, err := http.NewRequest(http.MethodGet, actualURL, nil)
	if err != nil {
		fail(err)
		return
	}
	req = req.WithContext(context.WithValue(req.Context(), origHostKey{}, req.URL.Host))
	resp, err := p.client.Do(req)
	if err != nil {
		failErr(err)
		return
	}
	defer resp.Body.Close()

	body, size, sizeTruncated, bodyTruncated, err := readBody(resp)
	if err != nil {
		failErr(err)
		return
	}
	if !p.isExpectedStatusCode(resp.StatusCode) {
		fail(fmt.Errorf("unexpected status code: %d", resp.StatusCode))
	} else if p.config.ExpectBody != "" && p.config.ExpectBody != strings.TrimRight(string(body), "\n") {
		msg := "invalid body"
		if bodyTruncated {
			msg += " (response body truncated at 1 MiB)"
		}
		fail(errors.New(msg))
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
				StatusCode:    resp.StatusCode,
				ResponseSize:  size,
				SizeTruncated: sizeTruncated,
				Headers:       headers,
				Redirects:     redirects,
			},
		}

		r <- successEvent(target, target, now, time.Since(now), details)
	}
}

// readBody reads up to maxBodyBytes of the body for expect_body matching and
// determines the real response size. With a known Content-Length that is the
// size; otherwise the remainder is read and discarded, up to maxCountBytes,
// beyond which the size is reported as maxCountBytes with sizeTruncated set.
// bodyTruncated reports that body is cut at maxBodyBytes.
func readBody(resp *http.Response) (body []byte, size int64, sizeTruncated, bodyTruncated bool, err error) {
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, 0, false, false, err
	}
	if len(body) <= maxBodyBytes {
		return body, int64(len(body)), false, false, nil
	}
	body = body[:maxBodyBytes]
	if resp.ContentLength >= 0 {
		return body, resp.ContentLength, false, true, nil
	}
	// maxBodyBytes+1 bytes are already read; one byte past the cap shows
	// that the body is larger than maxCountBytes.
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxCountBytes-maxBodyBytes))
	if err != nil {
		return nil, 0, false, false, err
	}
	size = maxBodyBytes + 1 + n
	if size > maxCountBytes {
		return body, maxCountBytes, true, true, nil
	}
	return body, size, false, true, nil
}

func (p *HTTPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	p.client.Timeout = timeout
	return runPeriodic(p.loop, r, p.probeType, interval, p.targets,
		func(target string) (string, string) { return target, target },
		func(target string) { p.probe(r, target) })
}

// Stop stops probing, waits for Start to return and closes idle keep-alive
// connections. It is safe to call more than once.
func (p *HTTPProber) Stop() {
	p.loop.stop()
	p.client.CloseIdleConnections()
}

// hostOverride returns the value of the Host entry in headers, matched
// case-insensitively. Validate rejects several entries; if there still are
// some, the lexically smallest key wins so the result is deterministic.
func hostOverride(headers map[string]string) string {
	var keys []string
	for k := range headers {
		if http.CanonicalHeaderKey(k) == "Host" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	return headers[slices.Min(keys)]
}

func (c *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(c.headers) == 0 {
		return c.transport.RoundTrip(req)
	}
	// A RoundTripper must not modify the caller's request.
	req = req.Clone(req.Context())
	for k, v := range c.headers {
		if http.CanonicalHeaderKey(k) == "Host" {
			continue // handled below
		}
		req.Header.Set(k, v)
	}
	tr := c.transport
	// net/http ignores Header["Host"]; the Host header is taken from req.Host.
	// Override it (and SNI) only for the original host, not for redirects
	// that leave it.
	if c.host != "" {
		if orig, _ := req.Context().Value(origHostKey{}).(string); orig == "" || orig == req.URL.Host {
			req.Host = c.host
			tr = c.hostTransport
		}
	}
	return tr.RoundTrip(req)
}

// CloseIdleConnections closes idle keep-alive connections of the inner
// transports (http.Client.CloseIdleConnections looks for this method).
func (c *customTransport) CloseIdleConnections() {
	for _, tr := range []http.RoundTripper{c.transport, c.hostTransport} {
		if ci, ok := tr.(interface{ CloseIdleConnections() }); ok {
			ci.CloseIdleConnections()
		}
	}
}

// Validate validates the HTTP configuration
func (cfg *HTTPConfig) Validate() error {
	hosts := 0
	for k := range cfg.Header {
		if http.CanonicalHeaderKey(k) == "Host" {
			hosts++
		}
	}
	if hosts > 1 {
		return errors.New("headers: multiple Host entries (differing only in case); specify Host once")
	}
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
