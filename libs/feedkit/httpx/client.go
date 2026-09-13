// Package httpx is a small HTTP client for feed polling: timeouts, a
// descriptive User-Agent, per-client rate limiting, and retries with
// exponential backoff that honor Retry-After.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Client wraps http.Client with retry and rate-limit policy.
type Client struct {
	HTTP       *http.Client
	UserAgent  string
	MaxRetries int           // retries after the first attempt (default 3)
	BaseDelay  time.Duration // first backoff (default 1s); grows 4x per retry
	MaxBody    int64         // response size cap (default 32 MiB)
	limiter    *rate.Limiter
}

// Option configures a Client.
type Option func(*Client)

// WithRateLimit caps requests to r per second with burst b.
func WithRateLimit(r rate.Limit, b int) Option {
	return func(c *Client) { c.limiter = rate.NewLimiter(r, b) }
}

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.HTTP.Timeout = d }
}

// WithRetries sets the number of retries after the first attempt.
func WithRetries(n int) Option {
	return func(c *Client) { c.MaxRetries = n }
}

// New returns a client with a 15s timeout, 3 retries, and the given User-Agent.
// The underlying transport honors HTTP(S)_PROXY from the environment.
func New(userAgent string, opts ...Option) *Client {
	c := &Client{
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		UserAgent:  userAgent,
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxBody:    32 << 20,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// StatusError is returned for non-2xx responses that were not retried into success.
type StatusError struct {
	Status int
	URL    string
	Body   string // first 512 bytes, for diagnostics
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpx: %s: HTTP %d: %s", e.URL, e.Status, e.Body)
}

// Get performs a GET with retries and returns the body.
func (c *Client) Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	return c.Do(ctx, http.MethodGet, url, nil, headers)
}

// Do performs a request with retries. body, if non-nil, must be re-readable
// (it is only used for idempotent feed calls; a bytes.Reader is recommended).
func (c *Client) Do(ctx context.Context, method, url string, body io.ReadSeeker, headers map[string]string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.backoff(attempt, lastErr)); err != nil {
				return nil, err
			}
			if body != nil {
				if _, err := body.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
			}
		}
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}
		data, err := c.once(ctx, method, url, body, headers)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("httpx: giving up after %d attempts: %w", c.MaxRetries+1, lastErr)
}

func (c *Client) once(ctx context.Context, method, url string, body io.Reader, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json, application/xml, text/xml, */*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &transientError{err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody))
	if err != nil {
		return nil, &transientError{err}
	}
	if resp.StatusCode/100 == 2 {
		return data, nil
	}
	se := &StatusError{Status: resp.StatusCode, URL: url, Body: string(truncate(data, 512))}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, &retryStatusError{se, parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return nil, se
}

type transientError struct{ err error }

func (e *transientError) Error() string { return "httpx: " + e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

type retryStatusError struct {
	*StatusError
	retryAfter time.Duration
}

func (e *retryStatusError) Unwrap() error { return e.StatusError }

func retryable(err error) bool {
	var t *transientError
	var r *retryStatusError
	return errors.As(err, &t) || errors.As(err, &r)
}

func (c *Client) backoff(attempt int, last error) time.Duration {
	var r *retryStatusError
	if errors.As(last, &r) && r.retryAfter > 0 {
		return r.retryAfter
	}
	d := c.BaseDelay
	for i := 1; i < attempt; i++ {
		d *= 4
	}
	// ±20% jitter so many pollers don't retry in lockstep.
	jitter := time.Duration(rand.Int64N(int64(d)/5+1)) - d/10
	return d + jitter
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
