package quote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Binance data source.

// binanceInterval maps a Period to Binance's kline interval.
//
// Binance supports every Period this package defines, including Day3, which no
// other source here serves. The terminal error exists so an unrecognized Period
// value reports itself rather than silently returning daily bars, which is what
// the removed 2017 implementation did for Hour6.
func binanceInterval(period Period) (string, error) {
	switch period {
	case Min1:
		return "1m", nil
	case Min3:
		return "3m", nil
	case Min5:
		return "5m", nil
	case Min15:
		return "15m", nil
	case Min30:
		return "30m", nil
	case Min60:
		return "1h", nil
	case Hour2:
		return "2h", nil
	case Hour4:
		return "4h", nil
	case Hour6:
		return "6h", nil
	case Hour8:
		return "8h", nil
	case Hour12:
		return "12h", nil
	case Daily, "":
		return "1d", nil
	case Day3:
		return "3d", nil
	case Weekly:
		return "1w", nil
	case Monthly:
		return "1M", nil
	}
	return "", fmt.Errorf("period %q is not supported by binance", period)
}

// binanceMaxBars is the most klines one request returns. Binance clamps a
// larger limit silently instead of reporting an error - limit=1500 comes back
// with 1000 rows and no indication it was truncated - so paging must advance
// from the last bar actually returned, never from the requested window.
const binanceMaxBars = 1000

// binanceInvalidSymbol is the error code Binance returns for an unknown symbol.
const binanceInvalidSymbol = -1121

// binancePageDelay is the pause between kline pages.
var binancePageDelay = time.Second

// binanceAPIError is the envelope Binance returns with a 4xx response.
type binanceAPIError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// binanceError converts a transport error into a SymbolNotFoundError when
// Binance reported an unknown symbol.
//
// Unlike Tiingo, Binance answers a bad symbol with 400 and a code in the body
// rather than a 404, so the status alone cannot distinguish "no such symbol"
// from any other malformed request.
func binanceError(err error, symbol string) error {
	var he *HTTPError
	if !errors.As(err, &he) {
		return err
	}
	var apiErr binanceAPIError
	if json.Unmarshal(he.Body, &apiErr) == nil && apiErr.Code == binanceInvalidSymbol {
		return &SymbolNotFoundError{Symbol: symbol}
	}
	return err
}

// binanceFloat decodes one of Binance's string-encoded numbers. Prices and
// volumes arrive as JSON strings, not numbers.
func binanceFloat(raw json.RawMessage) (float64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s, 64)
}

// quoteFromBinanceRows converts raw kline rows into a Quote.
//
// Bars are stamped with openTime (field 0). The removed implementation used
// closeTime (field 6), which labelled every bar at the end of its interval
// instead of the start, putting it a full period out of step with every other
// source in this package.
func quoteFromBinanceRows(symbol string, rows [][]json.RawMessage) (Quote, error) {
	q := NewQuote(symbol, len(rows))
	q.Precision = PrecisionCrypto

	for i, row := range rows {
		// [openTime, open, high, low, close, volume, closeTime, ...]
		if len(row) < 6 {
			return q, fmt.Errorf("binance kline %d for %s has %d fields, want at least 6", i, symbol, len(row))
		}

		var openTime int64
		if err := json.Unmarshal(row[0], &openTime); err != nil {
			return q, fmt.Errorf("parsing binance open time for %s: %w", symbol, err)
		}
		// .UTC() matters: the CSV format carries no zone, so a local wall clock
		// written out and parsed back as UTC shifts the instant.
		q.Date[i] = time.UnixMilli(openTime).UTC()

		for j, dst := range []*float64{&q.Open[i], &q.High[i], &q.Low[i], &q.Close[i], &q.Volume[i]} {
			v, err := binanceFloat(row[j+1])
			if err != nil {
				return q, fmt.Errorf("parsing binance kline field %d for %s: %w", j+1, symbol, err)
			}
			*dst = v
		}
	}
	return q, nil
}

// Binance returns Binance historical prices for a symbol.
func (c *Client) Binance(ctx context.Context, symbol string, start, end time.Time, period Period) (Quote, error) {
	interval, err := binanceInterval(period)
	if err != nil {
		return NewQuote("", 0), err
	}

	quote := NewQuote(symbol, 0)
	// Every Binance pair is crypto, including the TRY- and EUR-quoted ones the
	// symbol-name heuristic would otherwise round to 2 decimals.
	quote.Precision = PrecisionCrypto

	cursor := start
	for !cursor.After(end) {
		reqURL := fmt.Sprintf(
			"%s/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=%d",
			c.binanceURL(),
			url.QueryEscape(strings.ToUpper(symbol)),
			interval,
			cursor.UnixMilli(),
			end.UnixMilli(),
			binanceMaxBars)

		body, err := c.get(ctx, reqURL, nil)
		if err != nil {
			err = binanceError(err, symbol)
			c.logger().Printf("binance error: %v\n", err)
			return NewQuote("", 0), err
		}

		var rows [][]json.RawMessage
		if err := json.Unmarshal(body, &rows); err != nil {
			c.logger().Printf("binance error: %v\n", err)
			return NewQuote("", 0), fmt.Errorf("parsing binance klines for %s: %w", symbol, err)
		}
		if len(rows) == 0 {
			break
		}

		page, err := quoteFromBinanceRows(symbol, rows)
		if err != nil {
			c.logger().Printf("binance error: %v\n", err)
			return NewQuote("", 0), err
		}
		quote.Date = append(quote.Date, page.Date...)
		quote.Open = append(quote.Open, page.Open...)
		quote.High = append(quote.High, page.High...)
		quote.Low = append(quote.Low, page.Low...)
		quote.Close = append(quote.Close, page.Close...)
		quote.Volume = append(quote.Volume, page.Volume...)

		if len(rows) < binanceMaxBars {
			break
		}

		// Resume just past the last bar received rather than stepping by
		// interval*limit: because the limit clamps silently, a computed step
		// would skip every bar a short page did not cover.
		next := page.Date[len(page.Date)-1].Add(time.Millisecond)
		if !next.After(cursor) {
			// The cursor did not advance; stop rather than request forever.
			break
		}
		cursor = next

		// Binance rate limits by request weight; pause between pages.
		if err := sleep(ctx, binancePageDelay); err != nil {
			return quote, err
		}
	}

	return quote, nil
}

// BinanceSyms fetches Binance prices for a list of symbols.
func (c *Client) BinanceSyms(ctx context.Context, symbols []string, from, to time.Time, period Period) (Quotes, error) {
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		return c.Binance(ctx, symbol, from, to, period)
	})
}

// NewQuoteFromBinance - Binance historical prices for a symbol
//
// Deprecated: use Client.Binance, which takes a context.Context.
func NewQuoteFromBinance(symbol string, startDate, endDate string, period Period) (Quote, error) {
	start := ParseDateString(startDate)
	end := ParseDateString(endDate)
	return DefaultClient.Binance(context.Background(), symbol, start, end, period)
}

// NewQuotesFromBinance - create a list of prices from symbols in file
func NewQuotesFromBinance(filename string, startDate, endDate string, period Period) (Quotes, error) {
	symbols, err := NewSymbolsFromFile(filename)
	if err != nil {
		return Quotes{}, err
	}
	return NewQuotesFromBinanceSyms(symbols, startDate, endDate, period)
}

// NewQuotesFromBinanceSyms - create a list of prices from symbols in string array
//
// Deprecated: use Client.BinanceSyms.
func NewQuotesFromBinanceSyms(symbols []string, startDate, endDate string, period Period) (Quotes, error) {
	return DefaultClient.BinanceSyms(context.Background(), symbols,
		ParseDateString(startDate), ParseDateString(endDate), period)
}
