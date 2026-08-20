package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Tiingo daily and crypto data sources.

// tiingoDailyURL builds the Tiingo daily prices endpoint for a symbol.
// Both the quote fetch and the update-mode raw fetch used to build this string
// independently, with the same TrimSpace/slash-replacement dance.
func (c *Client) tiingoDailyURL(symbol string, from, to time.Time, period Period) string {
	u := fmt.Sprintf(
		"%s/tiingo/daily/%s/prices?startDate=%s&endDate=%s",
		c.tiingoURL(),
		strings.TrimSpace(strings.ReplaceAll(symbol, "/", "-")),
		url.QueryEscape(from.Format("2006-1-2")),
		url.QueryEscape(to.Format("2006-1-2")))

	switch period {
	case Weekly:
		u += "&resampleFreq=weekly"
	case Monthly:
		u += "&resampleFreq=monthly"
	}
	return u
}

func tiingoAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", fmt.Sprintf("Token %s", token))
	return h
}

// fetchTiingoDaily returns the raw Tiingo daily bars for a symbol.
func (c *Client) fetchTiingoDaily(ctx context.Context, symbol string, from, to time.Time, period Period, token string) ([]tquoteRaw, error) {
	body, err := c.get(ctx, c.tiingoDailyURL(symbol, from, to, period), tiingoAuthHeader(c.token(token)))
	if err != nil {
		if code, ok := statusCode(err); ok && code == http.StatusNotFound {
			return nil, &SymbolNotFoundError{Symbol: symbol}
		}
		return nil, err
	}

	var bars []tquoteRaw
	if err := json.Unmarshal(body, &bars); err != nil {
		return nil, fmt.Errorf("parsing tiingo daily json for %s: %w", symbol, err)
	}
	return bars, nil
}

// TiingoDaily returns Tiingo daily historical prices for a symbol.
func (c *Client) TiingoDaily(ctx context.Context, symbol string, from, to time.Time, period Period, token string) (Quote, error) {
	bars, err := c.fetchTiingoDaily(ctx, symbol, from, to, period, token)
	if err != nil {
		c.logger().Printf("tiingo error: %v\n", err)
		return NewQuote("", 0), err
	}
	return quoteFromTiingoBars(symbol, bars), nil
}

// quoteFromTiingoBars converts raw Tiingo bars into a Quote using adjusted prices.
func quoteFromTiingoBars(symbol string, bars []tquoteRaw) Quote {
	quote := NewQuote(symbol, len(bars))
	for i, b := range bars {
		quote.Date[i], _ = parseTiingoDate(b.Date)
		quote.Open[i] = b.AdjOpen
		quote.High[i] = b.AdjHigh
		quote.Low[i] = b.AdjLow
		quote.Close[i] = b.AdjClose
		quote.Volume[i] = b.Volume
	}
	return quote
}

// parseTiingoDate parses the leading YYYY-MM-DD of a Tiingo timestamp.
// Slicing [0:10] unguarded panicked on a short or empty date field.
func parseTiingoDate(s string) (time.Time, error) {
	if len(s) < 10 {
		return time.Time{}, fmt.Errorf("short tiingo date %q", s)
	}
	return time.Parse("2006-01-02", s[0:10])
}

func tiingoDaily(symbol string, from, to time.Time, period Period, token string) (Quote, error) {
	return DefaultClient.TiingoDaily(context.Background(), symbol, from, to, period, token)
}

// tquoteRaw mirrors Tiingo daily response for fields we care about.
type tquoteRaw struct {
	AdjClose    float64 `json:"adjClose"`
	AdjHigh     float64 `json:"adjHigh"`
	AdjLow      float64 `json:"adjLow"`
	AdjOpen     float64 `json:"adjOpen"`
	AdjVolume   float64 `json:"adjVolume"`
	Close       float64 `json:"close"`
	Date        string  `json:"date"`
	DivCash     float64 `json:"divCash"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Open        float64 `json:"open"`
	SplitFactor float64 `json:"splitFactor"`
	Volume      float64 `json:"volume"`
}

// tiingoCryptoPriceData is one crypto bar as returned by Tiingo.
type tiingoCryptoPriceData struct {
	TradesDone     float64 `json:"tradesDone"`
	Close          float64 `json:"close"`
	VolumeNotional float64 `json:"volumeNotional"`
	Low            float64 `json:"low"`
	Open           float64 `json:"open"`
	Date           string  `json:"date"` // "2017-12-19T00:00:00Z"
	High           float64 `json:"high"`
	Volume         float64 `json:"volume"`
}

type tiingoCryptoData struct {
	Ticker        string                  `json:"ticker"`
	BaseCurrency  string                  `json:"baseCurrency"`
	QuoteCurrency string                  `json:"quoteCurrency"`
	PriceData     []tiingoCryptoPriceData `json:"priceData"`
}

// tiingoCryptoResampleFreq maps a Period to Tiingo's crypto resampleFreq.
// Unsupported periods report an error rather than silently returning daily
// bars, which is what the old switch default did.
func tiingoCryptoResampleFreq(period Period) (string, error) {
	switch period {
	case Min1:
		return "1min", nil
	case Min3:
		return "3min", nil
	case Min5:
		return "5min", nil
	case Min15:
		return "15min", nil
	case Min30:
		return "30min", nil
	case Min60:
		return "1hour", nil
	case Hour2:
		return "2hour", nil
	case Hour4:
		return "4hour", nil
	case Hour6:
		return "6hour", nil
	case Hour8:
		return "8hour", nil
	case Hour12:
		return "12hour", nil
	case Daily, "":
		return "1day", nil
	}
	return "", fmt.Errorf("period %q is not supported by tiingo-crypto", period)
}

// TiingoCrypto returns Tiingo crypto historical prices for a symbol.
func (c *Client) TiingoCrypto(ctx context.Context, symbol string, from, to time.Time, period Period, token string) (Quote, error) {
	resampleFreq, err := tiingoCryptoResampleFreq(period)
	if err != nil {
		return NewQuote("", 0), err
	}

	reqURL := fmt.Sprintf(
		"%s/tiingo/crypto/prices?tickers=%s&startDate=%s&endDate=%s&resampleFreq=%s",
		c.tiingoURL(),
		url.QueryEscape(symbol),
		url.QueryEscape(from.Format("2006-1-2")),
		url.QueryEscape(to.Format("2006-1-2")),
		resampleFreq)

	body, err := c.get(ctx, reqURL, tiingoAuthHeader(c.token(token)))
	if err != nil {
		if code, ok := statusCode(err); ok && code == http.StatusNotFound {
			err = &SymbolNotFoundError{Symbol: symbol}
		}
		c.logger().Printf("tiingo crypto symbol '%s' error: %v\n", symbol, err)
		return NewQuote("", 0), err
	}

	var crypto []tiingoCryptoData
	if err := json.Unmarshal(body, &crypto); err != nil {
		c.logger().Printf("tiingo crypto symbol '%s' error: %v\n", symbol, err)
		return NewQuote("", 0), fmt.Errorf("parsing tiingo crypto json for %s: %w", symbol, err)
	}
	if len(crypto) < 1 {
		// Previously this returned a nil error, so a symbol with no data was
		// indistinguishable from a successful empty fetch.
		c.logger().Printf("tiingo crypto symbol '%s' no data returned\n", symbol)
		return NewQuote("", 0), &SymbolNotFoundError{Symbol: symbol}
	}

	bars := crypto[0].PriceData
	quote := NewQuote(symbol, len(bars))
	for i, b := range bars {
		quote.Date[i], _ = time.Parse(time.RFC3339, b.Date)
		quote.Open[i] = b.Open
		quote.High[i] = b.High
		quote.Low[i] = b.Low
		quote.Close[i] = b.Close
		quote.Volume[i] = b.Volume
	}
	return quote, nil
}

func tiingoCrypto(symbol string, from, to time.Time, period Period, token string) (Quote, error) {
	return DefaultClient.TiingoCrypto(context.Background(), symbol, from, to, period, token)
}

// NewQuoteFromTiingo - Tiingo daily historical prices for a symbol
//
// Deprecated: use Client.TiingoDaily, which takes a context.Context.
func NewQuoteFromTiingo(symbol, startDate, endDate string, period Period, token string) (Quote, error) {

	from := ParseDateString(startDate)
	to := ParseDateString(endDate)

	return tiingoDaily(symbol, from, to, period, token)
}

// NewQuoteFromTiingoCrypto - Tiingo crypto historical prices for a symbol
//
// Deprecated: use Client.TiingoCrypto, which takes a context.Context.
func NewQuoteFromTiingoCrypto(symbol, startDate, endDate string, period Period, token string) (Quote, error) {

	from := ParseDateString(startDate)
	to := ParseDateString(endDate)

	return tiingoCrypto(symbol, from, to, period, token)
}

// TiingoDailySyms fetches Tiingo daily prices for a list of symbols.
func (c *Client) TiingoDailySyms(ctx context.Context, symbols []string, from, to time.Time, period Period, token string) (Quotes, error) {
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		return c.TiingoDaily(ctx, symbol, from, to, period, token)
	})
}

// TiingoCryptoSyms fetches Tiingo crypto prices for a list of symbols.
func (c *Client) TiingoCryptoSyms(ctx context.Context, symbols []string, from, to time.Time, period Period, token string) (Quotes, error) {
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		return c.TiingoCrypto(ctx, symbol, from, to, period, token)
	})
}

// NewQuotesFromTiingoSyms - create a list of prices from symbols in string array
//
// Deprecated: use Client.TiingoDailySyms.
func NewQuotesFromTiingoSyms(symbols []string, startDate, endDate string, period Period, token string) (Quotes, error) {
	return DefaultClient.TiingoDailySyms(context.Background(), symbols,
		ParseDateString(startDate), ParseDateString(endDate), period, token)
}

// NewQuotesFromTiingoCryptoSyms - create a list of prices from symbols in string array
//
// Deprecated: use Client.TiingoCryptoSyms.
func NewQuotesFromTiingoCryptoSyms(symbols []string, startDate, endDate string, period Period, token string) (Quotes, error) {
	return DefaultClient.TiingoCryptoSyms(context.Background(), symbols,
		ParseDateString(startDate), ParseDateString(endDate), period, token)
}
