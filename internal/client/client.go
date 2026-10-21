// Package client is the shared HTTP client for the NDL Search APIs.
//
// It sets an explicit User-Agent, enforces a minimum interval between
// consecutive requests, and retries transient failures (5xx, 429 and
// connection errors) with exponential backoff.
package client

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the API root of the current (2024-) NDL Search domain.
	DefaultBaseURL = "https://ndlsearch.ndl.go.jp/api"
	// DefaultTimeout is the per-request timeout.
	DefaultTimeout = 30 * time.Second
	// DefaultInterval is the minimum wait between consecutive requests.
	DefaultInterval = time.Second
	// DefaultMaxRetries is the number of retries after the first attempt.
	DefaultMaxRetries = 3
	// RepoURL is advertised in the User-Agent.
	RepoURL = "https://github.com/nightswinger/ndl-cli"
)

// Config configures a Client.
type Config struct {
	BaseURL    string
	Timeout    time.Duration
	Interval   time.Duration
	UserAgent  string
	MaxRetries int
	// Backoff is the first retry delay; it doubles on every retry.
	Backoff time.Duration
	// Log receives one line per request when non-nil (--verbose).
	Log io.Writer
	// HTTPClient overrides the underlying client (tests).
	HTTPClient *http.Client
}

// Client performs rate-limited GET requests against the API.
type Client struct {
	cfg  Config
	http *http.Client

	mu   sync.Mutex
	last time.Time
}

// UserAgent builds the default User-Agent string for a version.
func UserAgent(version string) string {
	return fmt.Sprintf("ndl-cli/%s (+%s)", version, RepoURL)
}

// New returns a Client with defaults filled in.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Interval < 0 {
		cfg.Interval = 0
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = UserAgent("dev")
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: hc}
}

// URL returns the full request URL for an endpoint path ("/sru" etc.).
func (c *Client) URL(path string, q url.Values) string {
	u := c.cfg.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// Get fetches path with query q. The body is returned for any 2xx status.
// Non-2xx responses yield an *HTTPError that still carries the body, so
// callers can look for protocol-level diagnostics first. Connection
// failures yield a *NetworkError.
func (c *Client) Get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.URL(path, q)
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := c.cfg.Backoff << (attempt - 1)
			var he *HTTPError
			if errors.As(lastErr, &he) && he.RetryAfter > delay {
				delay = he.RetryAfter
			}
			c.logf("retry %d/%d in %s: %v", attempt, c.cfg.MaxRetries, delay, lastErr)
			if err := sleep(ctx, delay); err != nil {
				return nil, err
			}
		}
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		body, err := c.do(ctx, u)
		if err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/xml, text/xml;q=0.9, */*;q=0.1")
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.logf("GET %s -> error after %s: %v", u, time.Since(start).Round(time.Millisecond), err)
		return nil, &NetworkError{URL: u, Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	c.logf("GET %s -> %d (%d bytes, %s)", u, resp.StatusCode, len(body), time.Since(start).Round(time.Millisecond))
	if err != nil {
		return nil, &NetworkError{URL: u, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		he := &HTTPError{URL: u, StatusCode: resp.StatusCode, Body: body}
		he.Message = errorMessage(body)
		if s := resp.Header.Get("Retry-After"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				he.RetryAfter = time.Duration(n) * time.Second
			}
		}
		return nil, he
	}
	return body, nil
}

// wait blocks until at least Interval has elapsed since the previous request.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && c.cfg.Interval > 0 {
		if d := c.cfg.Interval - time.Since(c.last); d > 0 {
			if err := sleep(ctx, d); err != nil {
				return err
			}
		}
	}
	c.last = time.Now()
	return nil
}

func (c *Client) logf(format string, args ...any) {
	if c.cfg.Log != nil {
		fmt.Fprintf(c.cfg.Log, "[ndl] "+format+"\n", args...)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func retryable(err error) bool {
	var ne *NetworkError
	if errors.As(err, &ne) {
		return true
	}
	var he *HTTPError
	if errors.As(err, &he) {
		// 429 is how NDL Search signals its concurrent-request limit.
		return he.StatusCode >= 500 || he.StatusCode == http.StatusTooManyRequests
	}
	return false
}

// errorMessage extracts <error><message> from NDL's generic error body.
func errorMessage(body []byte) string {
	var e struct {
		XMLName xml.Name `xml:"error"`
		Code    string   `xml:"code"`
		Message string   `xml:"message"`
	}
	if xml.Unmarshal(body, &e) == nil && e.Message != "" {
		return strings.TrimSpace(e.Message)
	}
	return ""
}

// NetworkError is a transport-level failure (DNS, connect, timeout, ...).
type NetworkError struct {
	URL string
	Err error
}

func (e *NetworkError) Error() string { return fmt.Sprintf("network error: %v", e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

// HTTPError is a non-2xx HTTP response.
type HTTPError struct {
	URL        string
	StatusCode int
	Message    string
	Body       []byte
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("HTTP %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}
