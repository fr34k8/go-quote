/*
Package quote is a free quote downloader library and cli.

It downloads historical price quotes from Tiingo and Coinbase.

Copyright 2025 Mark Chenoweth
Licensed under terms of MIT license (see LICENSE)
*/
package quote

import (
	"io"
	"log"
	"strings"
	"time"
)

// Quote - structure for historical price data
type Quote struct {
	Symbol    string      `json:"symbol"`
	Precision int64       `json:"-"`
	Date      []time.Time `json:"date"`
	Open      []float64   `json:"open"`
	High      []float64   `json:"high"`
	Low       []float64   `json:"low"`
	Close     []float64   `json:"close"`
	Volume    []float64   `json:"volume"`
}

// Quotes - an array of historical price data
type Quotes []Quote

// Period - for quote history
type Period string

// ClientTimeout - connect/read timeout for client requests
//
// Deprecated: set Client.HTTP with the timeout you want.
const ClientTimeout = 10 * time.Second

const (
	// Min1 - 1 Minute time period
	Min1 Period = "60"
	// Min3 - 3 Minute time period
	Min3 Period = "3m"
	// Min5 - 5 Minute time period
	Min5 Period = "300"
	// Min15 - 15 Minute time period
	Min15 Period = "900"
	// Min30 - 30 Minute time period
	Min30 Period = "1800"
	// Min60 - 60 Minute time period
	Min60 Period = "3600"
	// Hour2 - 2 hour time period
	Hour2 Period = "2h"
	// Hour4 - 4 hour time period
	Hour4 Period = "4h"
	// Hour6 - 6 hour time period
	Hour6 Period = "6h"
	// Hour8 - 8 hour time period
	Hour8 Period = "8h"
	// Hour12 - 12 hour time period
	Hour12 Period = "12h"
	// Daily time period
	Daily Period = "d"
	// Day3 - 3 day time period
	Day3 Period = "3d"
	// Weekly time period
	Weekly Period = "w"
	// Monthly time period
	Monthly Period = "m"
)

// Log - standard logger, disabled by default
//
// Deprecated: set Client.Log instead. This package-level logger is shared
// mutable state; a Client carries its own.
var Log *log.Logger

// Delay - time delay in milliseconds between quote requests (default=100)
// Be nice, don't get blocked
//
// Note the units: despite the time.Duration type this holds a raw
// millisecond count, so Delay = 100 means 100ms. Client.Delay is an honest
// duration instead.
//
// Deprecated: set Client.Delay instead. This is shared mutable state, read
// without synchronization by the concurrent update workers.
var Delay time.Duration

func init() {
	Log = log.New(io.Discard, "quote: ", log.Ldate|log.Ltime|log.Lshortfile)
	Delay = 100
}

// NewQuote - new empty Quote struct
func NewQuote(symbol string, bars int) Quote {
	return Quote{
		Symbol: symbol,
		Date:   make([]time.Time, bars),
		Open:   make([]float64, bars),
		High:   make([]float64, bars),
		Low:    make([]float64, bars),
		Close:  make([]float64, bars),
		Volume: make([]float64, bars),
	}
}

// ParseDateString - parse a potentially partial date string to Time
func ParseDateString(dt string) time.Time {
	if dt == "" {
		// time.Parse below yields UTC, so use UTC here too: this function
		// used to return a local time for "" and a UTC time for everything
		// else, which formatted to different dates near midnight.
		return time.Now().UTC()
	}
	t, _ := time.Parse("2006-01-02 15:04", dt+"0000-01-01 00:00"[len(dt):])
	return t
}

// Decimal places used when formatting prices.
const (
	// PrecisionEquity - decimal places for equities
	PrecisionEquity = 2
	// PrecisionCrypto - decimal places for cryptocurrencies
	PrecisionCrypto = 8
)

// precision reports the decimal places to use when formatting q.
//
// A Quote returned by a data source carries the right value in Precision,
// because the source knows whether it deals in equities or crypto. Only a
// Quote built by hand or parsed from CSV/JSON falls back to guessing from the
// symbol, which is unreliable in both directions: a Coinbase pair quoted in
// EUR or GBP contains none of "BTC"/"ETH"/"USD" and was formatted to 2
// decimals, collapsing a whole day of sub-euro price action to a single
// value, while an equity ticker that happens to contain "USD" got 8.
func (q Quote) precision() int {
	if q.Precision > 0 {
		return int(q.Precision)
	}
	return getPrecision(q.Symbol)
}

// getPrecision guesses decimal places from a symbol name.
//
// Deprecated in spirit: prefer setting Quote.Precision at the source. This
// remains the fallback for Quotes that did not come from a provider.
func getPrecision(symbol string) int {
	precision := PrecisionEquity
	if strings.Contains(strings.ToUpper(symbol), "BTC") ||
		strings.Contains(strings.ToUpper(symbol), "ETH") ||
		strings.Contains(strings.ToUpper(symbol), "USD") {
		precision = PrecisionCrypto
	}
	return precision
}
