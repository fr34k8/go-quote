package quote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// ValidMarkets is the documented list and marketSources is what the code
// actually consults. The two drifting apart is exactly how "etf" became a
// market that validated but could never be fetched.
func TestMarketTableMatchesValidMarkets(t *testing.T) {
	documented := map[string]bool{}
	for _, m := range ValidMarkets {
		if documented[m] {
			t.Errorf("ValidMarkets lists %q twice", m)
		}
		documented[m] = true
	}

	for _, m := range Markets() {
		if !documented[m] {
			t.Errorf("market %q is in the table but missing from ValidMarkets", m)
		}
	}
	for m := range documented {
		if !ValidMarket(m) {
			t.Errorf("ValidMarkets lists %q but the table has no source for it", m)
		}
	}
}

// Every market must have a way to fetch it. The original bug was a market whose
// switch arm was missing, leaving an empty URL and an "unsupported protocol
// scheme" from the http client.
func TestEveryMarketHasASource(t *testing.T) {
	for name, src := range marketSources {
		if src.list != nil {
			continue // etf, over FTP
		}
		if src.url == nil {
			t.Errorf("market %q has no url builder", name)
		}
		if src.parse == nil {
			t.Errorf("market %q has no parser", name)
		}
	}
	// etf is the one non-HTTP source; it must still be reachable.
	if marketSources["etf"].list == nil {
		t.Error("etf must resolve through the FTP list function")
	}
}

func TestInvalidMarket(t *testing.T) {
	if ValidMarket("nope") {
		t.Error("ValidMarket(nope) = true")
	}
	_, err := DefaultClient.MarketList(context.Background(), "nope")
	if err == nil {
		t.Fatal("MarketList(nope) should fail")
	}
	// The message should tell the user what is valid.
	if !strings.Contains(err.Error(), "nasdaq") {
		t.Errorf("error %q should list the valid markets", err)
	}
}

func TestMarketRequiresToken(t *testing.T) {
	for _, m := range []string{"tiingo-btc", "tiingo-eth", "tiingo-usd"} {
		if !MarketRequiresToken(m) {
			t.Errorf("%s should require a token", m)
		}
	}
	// The old implementation guessed from a "tiingo" name prefix, so it could
	// not tell a credentialed source from one that merely looked like one.
	for _, m := range []string{"etf", "nasdaq", "coinbase", "binance-usdt", "nope"} {
		if MarketRequiresToken(m) {
			t.Errorf("%s should not require a token", m)
		}
	}
}

const binanceExchangeInfoBody = `{"symbols":[
 {"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT"},
 {"symbol":"ETHUSDT","status":"TRADING","baseAsset":"ETH","quoteAsset":"USDT"},
 {"symbol":"AAAUSDT","status":"BREAK","baseAsset":"AAA","quoteAsset":"USDT"},
 {"symbol":"BBBUSDT","status":"HALT","baseAsset":"BBB","quoteAsset":"USDT"},
 {"symbol":"ETHBTC","status":"TRADING","baseAsset":"ETH","quoteAsset":"BTC"},
 {"symbol":"DOGETRY","status":"TRADING","baseAsset":"DOGE","quoteAsset":"TRY"}
]}`

func TestGetBinanceMarket(t *testing.T) {
	usdt, err := getBinanceMarket("binance-usdt", binanceExchangeInfoBody)
	if err != nil {
		t.Fatalf("getBinanceMarket: %v", err)
	}
	// Sorted, quote-asset filtered, and status filtered: the removed version
	// checked none of the three, so delisted pairs came back as tradeable.
	want := []string{"BTCUSDT", "ETHUSDT"}
	if !slices.Equal(usdt, want) {
		t.Errorf("binance-usdt = %v, want %v", usdt, want)
	}

	btc, err := getBinanceMarket("binance-btc", binanceExchangeInfoBody)
	if err != nil {
		t.Fatalf("getBinanceMarket: %v", err)
	}
	if !slices.Equal(btc, []string{"ETHBTC"}) {
		t.Errorf("binance-btc = %v, want [ETHBTC]", btc)
	}
}

func TestGetBinanceMarketRejectsBadJSON(t *testing.T) {
	if _, err := getBinanceMarket("binance-usdt", "not json"); err == nil {
		t.Error("malformed json should be an error, not an empty list")
	}
}

// Market lists had no HTTP coverage at all before the table: the URLs were
// hardcoded absolute strings, so nothing could point them at a test server.
func TestMarketListOverHTTP(t *testing.T) {
	var gotPath, gotAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAgent = r.Header.Get("User-Agent")
		switch {
		case strings.Contains(r.URL.Path, "exchangeInfo"):
			w.Write([]byte(binanceExchangeInfoBody))
		case strings.Contains(r.URL.Path, "/products"):
			w.Write([]byte(`[{"id":"BTC-USD","trading_disabled":false},{"id":"OLD-USD","trading_disabled":true}]`))
		case strings.Contains(r.URL.Path, "list-type"):
			w.Write([]byte(`{"data":{"data":{"rows":[{"symbol":"AAPL"},{"symbol":"MSFT"}]}}}`))
		default:
			w.Write([]byte(`{"data":{"rows":[{"symbol":"SPY"},{"symbol":"QQQ"}]}}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	ctx := context.Background()

	for _, tc := range []struct {
		market   string
		wantPath string
		want     []string
	}{
		{"binance-usdt", "/api/v3/exchangeInfo", []string{"BTCUSDT", "ETHUSDT"}},
		{"coinbase", "/products", []string{"BTC-USD"}},
		{"nasdaq100", "/api/quote/list-type/nasdaq100", []string{"aapl", "msft"}},
		{"nasdaq", "/api/screener/stocks", []string{"qqq", "spy"}},
		{"technology", "/api/screener/stocks", []string{"qqq", "spy"}},
	} {
		got, err := c.MarketList(ctx, tc.market)
		if err != nil {
			t.Errorf("MarketList(%q): %v", tc.market, err)
			continue
		}
		if gotPath != tc.wantPath {
			t.Errorf("%s requested %q, want %q", tc.market, gotPath, tc.wantPath)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.market, got, tc.want)
		}
	}

	if gotAgent != marketListUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotAgent, marketListUserAgent)
	}
}

// The screener markets differ only by one query parameter; each must send its
// own, or every sector would return the same symbols.
func TestNasdaqScreenerSendsItsFilter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"data":{"rows":[]}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	for _, tc := range []struct{ market, want string }{
		{"nasdaq", "exchange=NASDAQ"},
		{"amex", "exchange=AMEX"},
		{"megacap", "marketcap=mega"},
		{"energy", "sector=energy"},
		{"technology", "sector=technology"},
	} {
		if _, err := c.MarketList(context.Background(), tc.market); err != nil {
			t.Fatalf("MarketList(%q): %v", tc.market, err)
		}
		if !strings.Contains(gotQuery, tc.want) {
			t.Errorf("%s query = %q, want it to contain %q", tc.market, gotQuery, tc.want)
		}
	}
}

// A tiingo market without a token must say so rather than sending "token=".
func TestTiingoMarketRequiresTokenBeforeRequest(t *testing.T) {
	t.Setenv("TIINGO_API_TOKEN", "")
	_, err := DefaultClient.MarketList(context.Background(), "tiingo-usd")
	if err == nil {
		t.Fatal("expected a missing-token error")
	}
	if !strings.Contains(err.Error(), "TIINGO_API_TOKEN") {
		t.Errorf("error %q should name the missing variable", err)
	}
}

func TestGetTiingoCryptoMarket(t *testing.T) {
	body := `[{"ticker":"btcusd","quoteCurrency":"usd"},
	          {"ticker":"ethbtc","quoteCurrency":"btc"},
	          {"ticker":"adausd","quoteCurrency":"usd"}]`
	got, err := getTiingoCryptoMarket("tiingo-usd", body)
	if err != nil {
		t.Fatalf("getTiingoCryptoMarket: %v", err)
	}
	if !slices.Equal(got, []string{"adausd", "btcusd"}) {
		t.Errorf("tiingo-usd = %v, want [adausd btcusd]", got)
	}
}
