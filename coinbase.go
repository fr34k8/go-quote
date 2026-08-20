package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Coinbase data source.

// coinbaseGranularity maps a Period to Coinbase's granularity in seconds.
// Periods Coinbase does not support report an error instead of silently
// falling back to daily bars, which is what the old switch default did.
func coinbaseGranularity(period Period) (int, error) {
	switch period {
	case Min1:
		return 60, nil
	case Min5:
		return 5 * 60, nil
	case Min15:
		return 15 * 60, nil
	case Min30:
		return 30 * 60, nil
	case Min60:
		return 60 * 60, nil
	case Daily, "":
		return 24 * 60 * 60, nil
	case Weekly:
		return 7 * 24 * 60 * 60, nil
	}
	return 0, fmt.Errorf("period %q is not supported by coinbase", period)
}

// coinbasePageDelay is the pause between Coinbase candle pages.
var coinbasePageDelay = time.Second

// Coinbase returns Coinbase historical prices for a symbol, paging through the
// 200-bar limit imposed by the candles endpoint.
func (c *Client) Coinbase(ctx context.Context, symbol string, start, end time.Time, period Period) (Quote, error) {
	granularity, err := coinbaseGranularity(period)
	if err != nil {
		return NewQuote("", 0), err
	}

	var quote Quote
	quote.Symbol = symbol
	// Every Coinbase product is a crypto pair, including the EUR- and
	// GBP-quoted ones the symbol-name heuristic could not recognize.
	quote.Precision = PrecisionCrypto

	const maxBars = 200
	step := time.Second * time.Duration(granularity)

	startBar := start
	endBar := startBar.Add(maxBars * step)
	if endBar.After(end) {
		endBar = end
	}

	for startBar.Before(end) {
		reqURL := fmt.Sprintf(
			"%s/products/%s/candles?start=%s&end=%s&granularity=%d",
			c.coinbaseURL(),
			url.PathEscape(symbol),
			url.QueryEscape(startBar.Format(time.RFC3339)),
			url.QueryEscape(endBar.Format(time.RFC3339)),
			granularity)

		body, err := c.get(ctx, reqURL, nil)
		if err != nil {
			c.logger().Printf("coinbase error: %v\n", err)
			return NewQuote("", 0), err
		}

		// Coinbase returns [time, low, high, open, close, volume] tuples.
		type cb [6]float64
		var bars []cb
		if err := json.Unmarshal(body, &bars); err != nil {
			c.logger().Printf("coinbase error: %v\n", err)
			return NewQuote("", 0), fmt.Errorf("parsing coinbase candles for %s: %w", symbol, err)
		}

		numrows := len(bars)
		q := NewQuote(symbol, numrows)
		for row := range numrows {
			bar := numrows - 1 - row // reverse the order
			// .UTC() matters: time.Unix returns a local time, and the CSV
			// format carries no zone, so a local wall clock written out and
			// parsed back as UTC shifted the instant by the local offset.
			q.Date[bar] = time.Unix(int64(bars[row][0]), 0).UTC()
			q.Low[bar] = bars[row][1]
			q.High[bar] = bars[row][2]
			q.Open[bar] = bars[row][3]
			q.Close[bar] = bars[row][4]
			q.Volume[bar] = bars[row][5]
		}
		quote.Date = append(quote.Date, q.Date...)
		quote.Low = append(quote.Low, q.Low...)
		quote.High = append(quote.High, q.High...)
		quote.Open = append(quote.Open, q.Open...)
		quote.Close = append(quote.Close, q.Close...)
		quote.Volume = append(quote.Volume, q.Volume...)

		startBar = endBar.Add(step)
		endBar = startBar.Add(maxBars * step)

		if startBar.Before(end) {
			// Coinbase is unauthenticated and rate limited; pause between pages.
			if err := sleep(ctx, coinbasePageDelay); err != nil {
				return quote, err
			}
		}
	}

	return quote, nil
}

// CoinbaseSyms fetches Coinbase prices for a list of symbols.
func (c *Client) CoinbaseSyms(ctx context.Context, symbols []string, from, to time.Time, period Period) (Quotes, error) {
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		return c.Coinbase(ctx, symbol, from, to, period)
	})
}

// NewQuoteFromCoinbase - Coinbase historical prices for a symbol
//
// Deprecated: use Client.Coinbase, which takes a context.Context.
func NewQuoteFromCoinbase(symbol, startDate, endDate string, period Period) (Quote, error) {
	start := ParseDateString(startDate)
	end := ParseDateString(endDate)
	return DefaultClient.Coinbase(context.Background(), symbol, start, end, period)
}

// NewQuotesFromCoinbase - create a list of prices from symbols in file
func NewQuotesFromCoinbase(filename, startDate, endDate string, period Period) (Quotes, error) {
	symbols, err := NewSymbolsFromFile(filename)
	if err != nil {
		return Quotes{}, err
	}
	return NewQuotesFromCoinbaseSyms(symbols, startDate, endDate, period)
}

// NewQuotesFromCoinbaseSyms - create a list of prices from symbols in string array
//
// Deprecated: use Client.CoinbaseSyms.
func NewQuotesFromCoinbaseSyms(symbols []string, startDate, endDate string, period Period) (Quotes, error) {
	return DefaultClient.CoinbaseSyms(context.Background(), symbols,
		ParseDateString(startDate), ParseDateString(endDate), period)
}
