package quote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
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
		binanceBase:  srv.URL,
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

// --- Binance ---

// One kline row: [openTime, open, high, low, close, volume, closeTime, ...].
// Prices and volumes are JSON strings and times are milliseconds.
func binanceRow(openTimeMs int64, open, high, low, cl, vol string) string {
	return fmt.Sprintf(`[%d,%q,%q,%q,%q,%q,%d,"0",0,"0","0","0"]`,
		openTimeMs, open, high, low, cl, vol, openTimeMs+86399999)
}

func TestClientBinance(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		fmt.Fprintf(w, "[%s,%s]",
			binanceRow(1704067200000, "42283.58", "45922.00", "42222.00", "44179.55", "27174.29903"),
			binanceRow(1704153600000, "44179.55", "45879.63", "44148.34", "44946.91", "65146.40661"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	q, err := c.Binance(context.Background(), "btcusdt", from, to, Daily)
	if err != nil {
		t.Fatalf("Binance: %v", err)
	}

	if gotPath != "/api/v3/klines" {
		t.Errorf("path = %q, want /api/v3/klines", gotPath)
	}
	// Symbols are upper-cased: Binance rejects "btcusdt" as an invalid symbol.
	if got := gotQuery.Get("symbol"); got != "BTCUSDT" {
		t.Errorf("symbol = %q, want BTCUSDT", got)
	}
	if got := gotQuery.Get("interval"); got != "1d" {
		t.Errorf("interval = %q, want 1d", got)
	}
	if got := gotQuery.Get("startTime"); got != "1704067200000" {
		t.Errorf("startTime = %q, want 1704067200000 (ms)", got)
	}
	if got := gotQuery.Get("limit"); got != "1000" {
		t.Errorf("limit = %q, want 1000", got)
	}

	if len(q.Close) != 2 {
		t.Fatalf("got %d bars, want 2", len(q.Close))
	}
	// Prices arrive as strings; they must be parsed, not dropped.
	if q.Open[0] != 42283.58 || q.High[0] != 45922.00 || q.Low[0] != 42222.00 {
		t.Errorf("bar 0 OHL = %v/%v/%v, want 42283.58/45922/42222", q.Open[0], q.High[0], q.Low[0])
	}
	if q.Close[1] != 44946.91 || q.Volume[1] != 65146.40661 {
		t.Errorf("bar 1 close/vol = %v/%v, want 44946.91/65146.40661", q.Close[1], q.Volume[1])
	}
	if q.Precision != PrecisionCrypto {
		t.Errorf("Precision = %d, want %d", q.Precision, PrecisionCrypto)
	}
	// openTime, not closeTime: the removed implementation used field 6, which
	// labelled every bar at the end of its interval instead of the start.
	if !q.Date[0].Equal(from) {
		t.Errorf("date[0] = %v, want %v (openTime)", q.Date[0], from)
	}
	if loc := q.Date[0].Location(); loc != time.UTC {
		t.Errorf("location = %v, want UTC", loc)
	}
}

// Binance reports an unknown symbol as HTTP 400 with a code in the body, not as
// a 404, so the status alone cannot identify it.
func TestClientBinanceSymbolNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":-1121,"msg":"Invalid symbol."}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).Binance(context.Background(), "NOTREAL",
		time.Now(), time.Now(), Daily)
	var snf *SymbolNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("err = %v, want SymbolNotFoundError", err)
	}
	if snf.Symbol != "NOTREAL" {
		t.Errorf("symbol = %q, want NOTREAL", snf.Symbol)
	}
}

// A 400 that is not -1121 is a different problem and must not be reported as a
// missing symbol.
func TestClientBinanceOtherErrorIsNotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":-1120,"msg":"Invalid interval."}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).Binance(context.Background(), "BTCUSDT",
		time.Now(), time.Now(), Daily)
	var snf *SymbolNotFoundError
	if errors.As(err, &snf) {
		t.Error("an invalid-interval error must not be reported as a missing symbol")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
}

// Binance clamps limit to 1000 silently. Paging must resume from the last bar
// actually returned, so a full page followed by a short one produces one
// contiguous series with no gap and no duplicate at the seam.
func TestClientBinancePagingAcrossFullPage(t *testing.T) {
	const day = 86400000
	var requests []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
		requests = append(requests, start)

		// Emit a full page of 1000 daily bars from startTime, then a short
		// second page of 3, then nothing.
		var n int
		switch len(requests) {
		case 1:
			n = binanceMaxBars
		case 2:
			n = 3
		}
		// Bars sit on interval boundaries: a startTime in the middle of a day
		// returns the next whole day, as the real API does. Without this the
		// stub would happily emit bars 1ms off the grid and hide a seam bug.
		first := (start + day - 1) / day * day
		rows := make([]string, n)
		for i := range n {
			rows[i] = binanceRow(first+int64(i)*day, "1", "2", "0.5", "1.5", "10")
		}
		fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	binancePageDelay = time.Nanosecond
	defer func() { binancePageDelay = time.Second }()

	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(5, 0, 0)
	q, err := c.Binance(context.Background(), "BTCUSDT", from, to, Daily)
	if err != nil {
		t.Fatalf("Binance: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("made %d requests, want 2 (stop after a short page)", len(requests))
	}
	// The second request must resume 1ms past the last bar of the first page,
	// not at from + 1000*interval computed from the request.
	wantSecond := from.UnixMilli() + int64(binanceMaxBars-1)*day + 1
	if requests[1] != wantSecond {
		t.Errorf("second startTime = %d, want %d (last openTime + 1ms)", requests[1], wantSecond)
	}

	if len(q.Close) != binanceMaxBars+3 {
		t.Fatalf("got %d bars, want %d", len(q.Close), binanceMaxBars+3)
	}
	// No duplicate and no gap at the page seam.
	seam := q.Date[binanceMaxBars-1]
	if got := q.Date[binanceMaxBars]; !got.Equal(seam.Add(24 * time.Hour)) {
		t.Errorf("bar after the seam = %v, want %v", got, seam.Add(24*time.Hour))
	}
	for i := 1; i < len(q.Date); i++ {
		if !q.Date[i].After(q.Date[i-1]) {
			t.Fatalf("dates not strictly increasing at %d: %v then %v", i, q.Date[i-1], q.Date[i])
		}
	}
}

func TestBinanceIntervalRejectsUnsupported(t *testing.T) {
	if _, err := binanceInterval(Period("nonsense")); err == nil {
		t.Error("an unknown period should be rejected, not treated as daily")
	}
	// Every period this package defines maps to a Binance interval.
	for _, name := range PeriodNames() {
		p, err := ParsePeriod(name)
		if err != nil {
			t.Fatalf("ParsePeriod(%q): %v", name, err)
		}
		if _, err := binanceInterval(p); err != nil {
			t.Errorf("binanceInterval(%q): %v", name, err)
		}
	}
	if got, err := binanceInterval(Day3); err != nil || got != "3d" {
		t.Errorf("Day3 = %q, %v, want 3d", got, err)
	}
	if got, err := binanceInterval(Hour6); err != nil || got != "6h" {
		t.Errorf("Hour6 = %q, %v, want 6h (the removed code returned daily)", got, err)
	}
}
