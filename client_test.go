package quote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client pointed at srv with no delay between requests.
func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		HTTP:         srv.Client(),
		Delay:        time.Nanosecond,
		tiingoBase:   srv.URL,
		coinbaseBase: srv.URL,
		nasdaqBase:   srv.URL,
	}
}

const tiingoDailyBody = `[
 {"date":"2018-07-12T00:00:00.000Z","adjOpen":278.28,"adjHigh":279.43,"adjLow":277.60,"adjClose":273.95,"volume":60124700,"splitFactor":1.0,"divCash":0.0},
 {"date":"2018-07-13T00:00:00.000Z","adjOpen":279.17,"adjHigh":279.93,"adjLow":278.66,"adjClose":274.17,"volume":48216000,"splitFactor":1.0,"divCash":0.0}
]`

func TestClientTiingoDaily(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Write([]byte(tiingoDailyBody))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	q, err := c.TiingoDaily(context.Background(), "spy",
		time.Date(2018, 7, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2018, 7, 13, 0, 0, 0, 0, time.UTC), Daily, "tok")
	if err != nil {
		t.Fatalf("TiingoDaily: %v", err)
	}
	if gotAuth != "Token tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Token tok")
	}
	if gotPath != "/tiingo/daily/spy/prices" {
		t.Errorf("path = %q", gotPath)
	}
	if q.Symbol != "spy" || len(q.Close) != 2 {
		t.Fatalf("got %s with %d bars, want spy with 2", q.Symbol, len(q.Close))
	}
	if q.Close[1] != 274.17 {
		t.Errorf("close[1] = %v, want 274.17", q.Close[1])
	}
	if !q.Date[0].Equal(time.Date(2018, 7, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("date[0] = %v", q.Date[0])
	}
}

// A 404 must surface as SymbolNotFoundError. It used to return a nil error
// alongside an empty quote, so a bad symbol looked like a successful fetch and
// got appended to results as an empty, unnamed quote.
func TestClientTiingoDailyNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).TiingoDaily(context.Background(), "nope",
		time.Now(), time.Now(), Daily, "tok")
	var snf *SymbolNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("err = %v, want SymbolNotFoundError", err)
	}
	if snf.Symbol != "nope" {
		t.Errorf("symbol = %q, want nope", snf.Symbol)
	}
}

func TestClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).get(context.Background(), srv.URL, nil)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if he.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", he.StatusCode)
	}
}

func TestClientRetriesOn503(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(tiingoDailyBody))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retry = RetryPolicy{Max: 3, Backoff: time.Millisecond}

	if _, err := c.TiingoDaily(context.Background(), "spy", time.Now(), time.Now(), Daily, "t"); err != nil {
		t.Fatalf("TiingoDaily: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

// A 4xx is not retryable: retrying a bad request just burns quota.
func TestClientDoesNotRetry4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retry = RetryPolicy{Max: 3, Backoff: time.Millisecond}

	if _, err := c.get(context.Background(), srv.URL, nil); err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestClientRespectsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	// Defers run LIFO, so release must be closed before srv.Close(), which
	// blocks until every in-flight handler returns.
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := newTestClient(srv).get(ctx, srv.URL, nil)
	if err == nil {
		t.Fatal("expected an error from the cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v; context deadline was not honored", elapsed)
	}
}

// FetchAll must skip failures rather than abort, and preserve input order.
func TestFetchAllSkipsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tiingo/daily/bad/prices" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Write([]byte(tiingoDailyBody))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	quotes, err := c.TiingoDailySyms(context.Background(),
		[]string{"spy", "bad", "aapl"}, time.Now(), time.Now(), Daily, "t")
	if err != nil {
		t.Fatalf("TiingoDailySyms: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("got %d quotes, want 2", len(quotes))
	}
	if quotes[0].Symbol != "spy" || quotes[1].Symbol != "aapl" {
		t.Errorf("got %s,%s want spy,aapl", quotes[0].Symbol, quotes[1].Symbol)
	}
}

func TestClientCoinbase(t *testing.T) {
	// [time, low, high, open, close, volume], newest first
	body := `[[1531440000,277.60,279.43,278.28,273.95,60124700]]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	start := time.Date(2018, 7, 13, 0, 0, 0, 0, time.UTC)
	q, err := c.Coinbase(context.Background(), "btc-usd", start, start.Add(24*time.Hour), Daily)
	if err != nil {
		t.Fatalf("Coinbase: %v", err)
	}
	if len(q.Close) != 1 {
		t.Fatalf("got %d bars, want 1", len(q.Close))
	}
	if q.Close[0] != 273.95 || q.Open[0] != 278.28 {
		t.Errorf("close=%v open=%v, want 273.95/278.28", q.Close[0], q.Open[0])
	}
}

func TestCoinbaseGranularityRejectsUnsupported(t *testing.T) {
	if _, err := coinbaseGranularity(Hour6); err == nil {
		t.Error("Hour6 should be rejected by coinbase, not silently treated as daily")
	}
	if g, err := coinbaseGranularity(Daily); err != nil || g != 86400 {
		t.Errorf("Daily = %d, %v", g, err)
	}
}

func TestTiingoCryptoResampleFreqRejectsUnsupported(t *testing.T) {
	if _, err := tiingoCryptoResampleFreq(Weekly); err == nil {
		t.Error("Weekly should be rejected by tiingo-crypto, not silently treated as daily")
	}
	if f, err := tiingoCryptoResampleFreq(Min15); err != nil || f != "15min" {
		t.Errorf("Min15 = %q, %v", f, err)
	}
}

// The deprecated package-level Delay holds raw milliseconds; Client.Delay is a
// real duration. Getting this wrong would turn a 500ms pause into 500ns and
// silently remove rate limiting.
func TestRateLimitUnits(t *testing.T) {
	old := Delay
	defer func() { Delay = old }()

	Delay = 500
	if got := (&Client{}).rateLimit(); got != 500*time.Millisecond {
		t.Errorf("legacy Delay=500 gave %v, want 500ms", got)
	}

	c := &Client{Delay: 250 * time.Millisecond}
	if got := c.rateLimit(); got != 250*time.Millisecond {
		t.Errorf("Client.Delay gave %v, want 250ms", got)
	}
}

func TestClientNilSafeAccessors(t *testing.T) {
	var c *Client
	if c.httpClient() == nil {
		t.Error("httpClient() returned nil")
	}
	if c.logger() == nil {
		t.Error("logger() returned nil")
	}
	if c.tiingoURL() != defaultTiingoBaseURL {
		t.Errorf("tiingoURL() = %q", c.tiingoURL())
	}
}
