package quote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Default API endpoints. Held as Client fields so tests can point at an
// httptest.Server; before this existed, the only injectable seam in the
// package was the tiingoFetch function variable.
const (
	defaultTiingoBaseURL   = "https://api.tiingo.com"
	defaultCoinbaseBaseURL = "https://api.exchange.coinbase.com"
	defaultNasdaqBaseURL   = "https://api.nasdaq.com"

	// Binance market data comes from data-api.binance.vision, not
	// api.binance.com. The latter returns HTTP 451 ("restricted location")
	// from outside the regions Binance serves, including the US - which is
	// why the original binance support, removed in 2024, had stopped working.
	// binance.vision serves the same /api/v3 endpoints for public market data
	// with no key and no geo restriction.
	//
	// api.binance.us is a different exchange with its own symbol universe, so
	// it is not a drop-in replacement for this base URL.
	defaultBinanceBaseURL = "https://data-api.binance.vision"
)

// defaultHTTPClient is shared by every Client that does not supply its own.
// The previous code allocated an http.Client per call - and, in the Coinbase
// pager, per 200-bar page - which defeated connection pooling.
var defaultHTTPClient = &http.Client{Timeout: ClientTimeout}

// RetryPolicy controls retries for transport errors, 429, and 5xx responses.
// The zero value performs no retries, which is the historical behavior; opt in
// by setting Max. A Retry-After header on a 429 takes precedence over Backoff.
type RetryPolicy struct {
	// Max is the number of additional attempts after the first.
	Max int
	// Backoff is the base delay, doubled after each attempt. Defaults to
	// 500ms when Max > 0 and Backoff is zero.
	Backoff time.Duration
}

func (p RetryPolicy) backoff(attempt int) time.Duration {
	base := p.Backoff
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	d := base << (attempt - 1)
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// HTTPError reports a non-2xx response from a data source.
type HTTPError struct {
	StatusCode int
	Status     string
	URL        string
	Body       []byte

	// retryAfter carries a parsed Retry-After header, when present.
	retryAfter    time.Duration
	hasRetryAfter bool
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %s for %s", e.Status, e.URL)
}

// Client holds the configuration for fetching quotes: which HTTP client to
// use, how long to pause between requests, credentials, and where to log.
//
// The package-level functions delegate to DefaultClient, so existing code
// keeps working unchanged. Prefer a Client for new code: it accepts a
// context.Context, allows a per-caller timeout and retry policy, and does not
// rely on mutable package state.
type Client struct {
	// HTTP is the underlying client. Defaults to a shared client with
	// ClientTimeout.
	HTTP *http.Client

	// Delay is the pause between consecutive symbol requests. Unlike the
	// deprecated package-level Delay, this is a real duration: use
	// 100*time.Millisecond, not 100.
	Delay time.Duration

	// Log receives progress and error messages. Defaults to the package-level
	// Log, which discards output unless SetOutput is called.
	Log *log.Logger

	// Token is the Tiingo API token used when a call does not supply one.
	Token string

	// UserAgent is sent with every request when non-empty.
	UserAgent string

	// Retry controls retry behavior. The zero value does not retry.
	Retry RetryPolicy

	// Base URLs, overridable in tests.
	tiingoBase   string
	coinbaseBase string
	nasdaqBase   string
	binanceBase  string
}

// DefaultClient backs all package-level functions.
var DefaultClient = &Client{}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTPClient
}

func (c *Client) logger() *log.Logger {
	if c != nil && c.Log != nil {
		return c.Log
	}
	return Log
}

// rateLimit reports the pause between symbol requests.
//
// When Client.Delay is unset this falls back to the deprecated package-level
// Delay, which for historical reasons holds a raw millisecond count rather
// than a duration - quote.Delay = 100 means 100ms. Converting here keeps that
// meaning intact for existing callers while letting Client.Delay be an honest
// duration.
func (c *Client) rateLimit() time.Duration {
	if c != nil && c.Delay != 0 {
		return c.Delay
	}
	return Delay * time.Millisecond
}

func (c *Client) token(override string) string {
	if override != "" {
		return override
	}
	if c != nil {
		return c.Token
	}
	return ""
}

func (c *Client) tiingoURL() string {
	if c != nil && c.tiingoBase != "" {
		return c.tiingoBase
	}
	return defaultTiingoBaseURL
}

func (c *Client) coinbaseURL() string {
	if c != nil && c.coinbaseBase != "" {
		return c.coinbaseBase
	}
	return defaultCoinbaseBaseURL
}

func (c *Client) nasdaqURL() string {
	if c != nil && c.nasdaqBase != "" {
		return c.nasdaqBase
	}
	return defaultNasdaqBaseURL
}

func (c *Client) binanceURL() string {
	if c != nil && c.binanceBase != "" {
		return c.binanceBase
	}
	return defaultBinanceBaseURL
}

// sleep pauses for d, returning early if ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
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

// retryAfter reads a Retry-After header expressed in seconds.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// get performs a GET and returns the response body.
//
// It centralizes what was previously duplicated or missing across the five
// call sites: the http.NewRequest error was discarded everywhere (`req, _ :=`),
// so a malformed URL panicked on the following header write; status codes were
// checked in only one of the five; and response bodies were closed with a
// deferred call inside a paging loop.
func (c *Client) get(ctx context.Context, url string, header http.Header) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	client := c.httpClient()
	attempts := 1 + max(c.Retry.Max, 0)

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			wait := c.Retry.backoff(attempt)
			var he *HTTPError
			if errors.As(lastErr, &he) && he.hasRetryAfter {
				wait = he.retryAfter
			}
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			// A bad URL will not fix itself; do not retry.
			return nil, err
		}
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		if c != nil && c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = readErr
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		httpErr := &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			URL:        url,
			Body:       body,
		}
		if d, ok := retryAfter(resp); ok {
			httpErr.retryAfter = d
			httpErr.hasRetryAfter = true
		}
		lastErr = httpErr

		if !retryableStatus(resp.StatusCode) {
			return nil, httpErr
		}
	}

	return nil, lastErr
}

// statusCode reports the HTTP status carried by err, if any.
func statusCode(err error) (int, bool) {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.StatusCode, true
	}
	return 0, false
}
