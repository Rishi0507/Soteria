package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultAPIVersion is the Admin API version used unless overridden.
// Shopify supports each quarterly version for 12 months.
const DefaultAPIVersion = "2026-01"

// Client talks to one shop's Admin GraphQL API.
type Client struct {
	shop       string // my-store.myshopify.com
	token      string
	version    string
	http       *http.Client
	log        *slog.Logger
	maxRetries int

	// leaky-bucket state from the last response's extensions.cost
	mu        sync.Mutex
	available float64   // points currently available
	restore   float64   // points restored per second
	maximum   float64   // bucket size
	observed  time.Time // when available was observed
}

// Option configures a Client.
type Option func(*Client)

// WithAPIVersion overrides DefaultAPIVersion.
func WithAPIVersion(v string) Option { return func(c *Client) { c.version = v } }

// WithHTTPClient replaces the HTTP client (tests, proxies).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// WithRetries sets retries for throttled / 5xx responses (default 5).
func WithRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// New returns a client for shop (e.g. "soteria-dev.myshopify.com") using an
// Admin API access token (shpat_…). The token is never logged.
func New(shop, token string, opts ...Option) (*Client, error) {
	shop = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(shop, "https://"), "http://"), "/")
	if shop == "" || !strings.Contains(shop, ".") {
		return nil, errors.New("shopify: shop must be like my-store.myshopify.com")
	}
	if token == "" {
		return nil, errors.New("shopify: empty access token")
	}
	c := &Client{
		shop:       shop,
		token:      token,
		version:    DefaultAPIVersion,
		http:       &http.Client{Timeout: 30 * time.Second},
		log:        slog.Default(),
		maxRetries: 5,
		// Optimistic defaults until the first response tells us the truth
		// (Basic plan: 100 points/s restore, 1000-point bucket).
		available: 1000, restore: 100, maximum: 1000, observed: time.Now(),
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Endpoint is the GraphQL URL.
func (c *Client) Endpoint() string {
	return fmt.Sprintf("https://%s/admin/api/%s/graphql.json", c.shop, c.version)
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data       json.RawMessage `json:"data"`
	Errors     GraphQLErrors   `json:"errors,omitempty"`
	Extensions struct {
		Cost struct {
			RequestedQueryCost float64 `json:"requestedQueryCost"`
			ActualQueryCost    float64 `json:"actualQueryCost"`
			ThrottleStatus     struct {
				MaximumAvailable   float64 `json:"maximumAvailable"`
				CurrentlyAvailable float64 `json:"currentlyAvailable"`
				RestoreRate        float64 `json:"restoreRate"`
			} `json:"throttleStatus"`
		} `json:"cost"`
	} `json:"extensions"`
}

// Do executes one GraphQL document and decodes data into out (may be nil).
// It waits for bucket capacity, retries THROTTLED and 5xx with backoff, and
// returns GraphQLErrors for other top-level errors.
func (c *Client) Do(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.backoff(attempt, lastErr)); err != nil {
				return err
			}
		}
		if err := c.waitForBudget(ctx, estimateCost(query)); err != nil {
			return err
		}
		resp, err := c.once(ctx, body)
		if err != nil {
			lastErr = err
			if isRetryable(err) {
				c.log.Warn("shopify request failed, retrying", "attempt", attempt+1, "err", err)
				continue
			}
			return err
		}
		c.observeCost(resp)
		if len(resp.Errors) > 0 {
			gerr := resp.Errors
			if gerr[0].Code() == "THROTTLED" || strings.Contains(strings.ToLower(gerr[0].Message), "throttled") {
				lastErr = gerr
				c.log.Warn("shopify throttled, retrying", "attempt", attempt+1)
				continue
			}
			return gerr
		}
		if out != nil && len(resp.Data) > 0 {
			if err := json.Unmarshal(resp.Data, out); err != nil {
				return fmt.Errorf("shopify: decode data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("shopify: giving up after %d attempts: %w", c.maxRetries+1, lastErr)
}

type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var r *retryableError
	return errors.As(err, &r)
}

func (c *Client) once(ctx context.Context, body []byte) (*gqlResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.token)
	req.Header.Set("User-Agent", "Soteria-Shopify/1 (+https://github.com/abhishek-pandey7)")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, &retryableError{err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, &retryableError{err}
	}
	switch {
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return nil, &retryableError{&HTTPError{Status: res.StatusCode, Body: truncate(data, 300)}}
	case res.StatusCode/100 != 2:
		return nil, &HTTPError{Status: res.StatusCode, Body: truncate(data, 300)}
	}
	var out gqlResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("shopify: decode response: %w (%s)", err, truncate(data, 200))
	}
	return &out, nil
}

// ---- cost-based throttling ----------------------------------------------

func (c *Client) observeCost(r *gqlResponse) {
	ts := r.Extensions.Cost.ThrottleStatus
	if ts.MaximumAvailable == 0 {
		return // no cost extension (e.g. error document)
	}
	c.mu.Lock()
	c.available, c.restore, c.maximum, c.observed = ts.CurrentlyAvailable, ts.RestoreRate, ts.MaximumAvailable, time.Now()
	c.mu.Unlock()
}

// waitForBudget sleeps until the bucket has at least need points (projected
// from the last observation and the restore rate).
func (c *Client) waitForBudget(ctx context.Context, need float64) error {
	c.mu.Lock()
	avail := c.available + c.restore*time.Since(c.observed).Seconds()
	if avail > c.maximum {
		avail = c.maximum
	}
	if need > c.maximum {
		need = c.maximum
	}
	var wait time.Duration
	if avail < need && c.restore > 0 {
		wait = time.Duration((need-avail)/c.restore*float64(time.Second)) + 50*time.Millisecond
	}
	// Reserve the points optimistically; the real cost is corrected on response.
	c.available = avail - need
	c.observed = time.Now()
	c.mu.Unlock()
	if wait > 0 {
		c.log.Debug("shopify: waiting for query budget", "wait", wait, "need", need)
		return sleep(ctx, wait)
	}
	return nil
}

// estimateCost is a rough pre-flight cost guess so a burst of paginated
// reads does not slam into the bucket: connections cost ~1 point per
// requested node; mutations ~10.
func estimateCost(query string) float64 {
	q := strings.ToLower(query)
	if strings.HasPrefix(strings.TrimSpace(q), "mutation") {
		return 10
	}
	cost := 1.0
	for _, n := range []string{"first: 250", "first:250"} {
		cost += float64(strings.Count(q, n)) * 250
	}
	for _, n := range []string{"first: 50", "first:50"} {
		cost += float64(strings.Count(q, n)) * 50
	}
	for _, n := range []string{"first: 10", "first:10"} {
		cost += float64(strings.Count(q, n)) * 10
	}
	return cost
}

func (c *Client) backoff(attempt int, last error) time.Duration {
	// Throttled: wait for a meaningful refill; otherwise exponential.
	if ge, ok := last.(GraphQLErrors); ok && len(ge) > 0 && ge[0].Code() == "THROTTLED" {
		c.mu.Lock()
		r := c.restore
		c.mu.Unlock()
		if r > 0 {
			return time.Duration(float64(time.Second)*(100/r)) + 100*time.Millisecond
		}
		return 2 * time.Second
	}
	d := 500 * time.Millisecond
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
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

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// userErrors converts a mutation's userErrors payload into an error.
func userErrors(mutation string, errs []UserError) error {
	if len(errs) == 0 {
		return nil
	}
	return &UserErrors{Mutation: mutation, Errors: errs}
}
