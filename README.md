# go-quote

[![GoDoc](http://godoc.org/github.com/markcheno/go-quote?status.svg)](http://godoc.org/github.com/markcheno/go-quote)

A free quote downloader library and cli

Downloads daily historical price quotes from Tiingo and daily/intraday data from various api's. Written in pure Go. No external dependencies. Now downloads crypto coin historical data from various exchanges.

- Update: 08/19/2026 - Added Binance support (`-source=binance`), restoring the source removed in 2024. Uses `data-api.binance.vision`, which needs no API key and is not geo-restricted; `api.binance.com` returns 451 outside eligible regions. Supports every period the package defines, including `3d`, which no other source offers

- Update: 08/19/2026 - Modernization: added `Client` (context, retries, shared HTTP client) and a `Provider` registry; split the library into topic files; requires Go 1.24+. Fixes: EUR/GBP-quoted Coinbase pairs were rounded to 2 decimals and lost their price data, all timestamps are now UTC, `-markets=etf` works from the library API, multi-symbol CSV parsing no longer scrambles symbols, and CSV round-trips no longer append a phantom zero bar. Existing API is unchanged; older entry points still work and are marked deprecated

- Update: 09/04/2025 - added Tiingo CSV update mode (-update) with 10-day backfill, optional full re-download on corporate actions, and -concurrency for faster updates

- Update: 04/01/2025 - added markets flag, wildcard input files, multiple inputs

- Update: 03/02/2025 - Removed obsolete Yahoo support

- Update: 02/15/2024 - Major update: updated to Go 1.22, removed bittrex/binance support, fixed nasdaq/tiingo markets

- Update: 11/15/2021 - Removed obsolete markets, converted to go modules

- Update: 7/18/2021 - Removed obsolete Google support

- Update: 6/26/2019 - updated GDAX to Coinbase, added coinbase market

- Update: 4/26/2018 - Added preliminary [tiingo](https://api.tiingo.com/) CRYPTO support. Use -source=tiingo-crypto -token=<your_tingo_token> You can also set env variable TIINGO_API_TOKEN. To get symbol lists, use market: tiingo-btc, tiingo-eth or tiingo-usd

- Update: 12/21/2017 - Added Amibroker format option (creates csv file with separate date and time). Use -format=ami

- Update: 12/20/2017 - Added [Binance](https://www.binance.com/trade.html) exchange support. Use -source=binance

- Update: 12/18/2017 - Added [Bittrex](https://bittrex.com/home/markets) exchange support. Use -source=bittrex

- Update: 10/21/2017 - Added Coinbase [GDAX](https://www.gdax.com/trade/BTC-USD) exchange support. Use -source=gdax All times are in UTC. Automatically rate limited.

- Update: 7/19/2017 - Added preliminary [tiingo](https://api.tiingo.com/) support. Use -source=tiingo -token=<your_tingo_token> You can also set env variable TIINGO_API_TOKEN

- Update: 5/24/2017 - Now works with the new Yahoo download format. Beware - Yahoo data quality is now questionable and the free Yahoo quotes are likely to permanently go away in the near future. Use with caution!

Still very much in alpha mode. Expect bugs and API changes. Comments/suggestions/pull requests welcome!

Copyright 2025 Mark Chenoweth

Install CLI utility (quote) with:

```bash
go install github.com/markcheno/go-quote/quote@latest
```

```
Usage:
  quote -h | -help
  quote -v | -version
  quote [-outfile=<outputFile>] <market>
  quote [-years=<years>|(-start=<datestr> [-end=<datestr>])] [options] [-infile=<filename>|<symbol> ...]
  quote -update=<path> [-end=<datestr>] [-backfill-days=<n>] [-full-redownload-on-ca] [-concurrency=<n>] [-token=<tiingo_token>]

Options:
  -h -help             show help
  -v -version          show version
  -years=<years>       number of years to download [default=5]
  -start=<datestr>     yyyy[-[mm-[dd]]]
  -end=<datestr>       yyyy[-[mm-[dd]]] [default=today]
  -markets=<list>      list of valid markets to download (comma separated)
  -infile=<filename>   list of symbols to download
  -outfile=<filename>  output filename
  -period=<period>     1m|3m|5m|15m|30m|1h|2h|4h|6h|8h|12h|d|3d|w|m [default=d]
  -source=<source>     tiingo|tiingo-crypto|coinbase|binance [default=tiingo]
  -token=<tiingo_tok>  tingo api token [default=TIINGO_API_TOKEN]
  -format=<format>     (csv|json|hs|ami) [default=csv]
  -all=<bool>          all in one file (true|false) [default=false]
  -log=<dest>          filename|stdout|stderr|discard [default=stdout]
  -delay=<ms>          delay in milliseconds between quote requests
  -update=<path>       update an existing Tiingo CSV (single or -all multi) in place
  -backfill-days=<n>   days of overlap to rewrite [default=10]
  -full-redownload-on-ca  if splits/dividends detected, fully redownload the symbol block
  -concurrency=<n>     concurrent symbol fetches in update mode [default=1]

Note: not all periods work with all sources

Valid markets:
etf,nasdaq,nasdaq100,amex,nyse,megacap,largecap,midcap,smallcap,microcap,nanocap,
telecommunications,health_care,finance,real_estate,consumer_discretionary,
consumer_staples,industrials,basic_materials,energy,utilities,technology
coinbase,tiingo-usd,tiingo-btc,tiingo-eth
```

## CLI Examples

```bash
# display usage
quote -help

# downloads 10 years of smallcap, midcap, largecap and megacap stocks to stocks.csv
quote -markets=smallcap,midcap,largecap,megacap -all=true -years=10 -outfile=stocks.csv

# downloads 10 years of spy,qqq and djia to indexes.csv
quote -years=10 -outfile=indexes.csv -all=true spy qqq djia

# downloads 5 years of Tiingo SPY history to spy.csv (TIINGO_API_TOKEN must be set)
quote spy

# downloads 1 year of bitcoin history to BTC-USD.csv
quote -years=1 -source=coinbase BTC-USD

# downloads 1 year of bitcoin history from Binance (no api key needed)
quote -years=1 -source=binance BTCUSDT

# 3-day bars, a period only Binance supports
quote -years=1 -source=binance -period=3d BTCUSDT


# downloads full etf symbol list to etf.txt, also works for nasdaq,nasdaq100,nyse,amex
quote etf

# download fresh etf list and 5 years of etf data all in one file
quote -markets=etf -all=true -outfile=etf.csv

# update a large multi-symbol CSV in place (Tiingo), 4 concurrent fetchers
quote -update=stocks.csv -concurrency=4

# update a single-symbol CSV inferred from filename (e.g., spy.csv)
quote -update=spy.csv -backfill-days=10 -full-redownload-on-ca

# control request pacing with -delay and concurrency
quote -delay=100 -update=stocks.csv -concurrency=8
```

Update mode rate limiting

- -delay=0: no global throttle; workers issue requests immediately.
- -delay>0: a single global limiter spaces requests across all workers by ~delay ms. Use higher -concurrency to overlap work while keeping polite pacing.

Tiingo rate limit guidance

- Start conservatively: `-delay=100..250` and `-concurrency=2..4`.
- Increase gradually and watch for `429 Too Many Requests` responses.
- Leave `-delay` > 0 to be polite; set `-delay=0` only if your plan allows higher throughput.

## Install library

Requires Go 1.24 or later. Install the package with:

```bash
go get github.com/markcheno/go-quote@latest
```

## Library example

```go
package main

import (
	"fmt"
	"github.com/markcheno/go-quote"
	"github.com/markcheno/go-talib"
)

func main() {
	spy, _ := quote.NewQuoteFromTiingo("spy", "2016-01-01", "2016-04-01", quote.Daily, "your-tiingo-token")
	fmt.Print(spy.CSV())
	rsi2 := talib.Rsi(spy.Close, 2)
	fmt.Println(rsi2)
}
```

### Using a Client

The package-level functions above still work, but new code should prefer a
`Client`. It takes a `context.Context`, so requests can be cancelled or given a
deadline, and it lets you set the request delay, retry policy, and HTTP client
per caller instead of through package globals:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/markcheno/go-quote"
)

func main() {
	c := &quote.Client{
		Token: "your-tiingo-token",
		Delay: 250 * time.Millisecond,
		Retry: quote.RetryPolicy{Max: 3}, // backs off on 429 and 5xx
	}

	p, err := c.Provider("tiingo") // or "tiingo-crypto", "coinbase", "binance"
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spy, err := p.Fetch(ctx, quote.Request{
		Symbol: "spy",
		From:   quote.ParseDateString("2016-01-01"),
		To:     quote.ParseDateString("2016-04-01"),
		Period: quote.Daily,
	})
	if err != nil {
		panic(err)
	}

	fmt.Print(spy.CSV())
}
```

Fetch many symbols with `Client.FetchSymbols`, which paces requests by
`Client.Delay` and skips symbols that fail:

```go
quotes, err := c.FetchSymbols(ctx, p, []string{"spy", "qqq", "dia"}, quote.Request{
	From:   quote.ParseDateString("2024-01-01"),
	To:     quote.ParseDateString("2024-12-31"),
	Period: quote.Daily,
})
if err != nil {
	panic(err)
}
err = quotes.WriteFile("indexes.csv", quote.FormatCSV)
```

`Client.Provider` reports the valid sources and `Provider.Periods` the periods
each one supports, so an unsupported combination is an error rather than a
silent fallback to daily bars.

## Notes

**Timestamps are UTC.** Every source returns UTC, so a Coinbase file and a
Tiingo file label the same instant identically. The CSV format carries no
timezone, so this also means a file written and read back keeps its instants.

**Decimal places** come from the data source: 2 for equities, 8 for
cryptocurrencies, recorded in `Quote.Precision`. CSV parsing infers it from the
decimals present in the file, so reading a file and writing it out again does
not round it.

**Adding a data source** means implementing `quote.Provider` and calling
`quote.RegisterProvider`; the CLI picks it up from the registry with no further
changes.

## Upgrading

Three changes need attention if you have existing data or scripts:

- **EUR/GBP-quoted Coinbase pairs** (`DOGE-EUR`, `ADA-GBP`, and ~48 others) were
  written with 2 decimal places, which rounded sub-euro prices away entirely -
  a full day of `DOGE-EUR` came out as `0.08,0.08,0.08,0.08`. The data in those
  files is gone and needs refetching, not reformatting.
- **Coinbase timestamps** were written in local time and are now UTC. If you are
  not in UTC they will shift by your offset, so refetch whole ranges rather than
  diffing old files against new ones. Tiingo files are unaffected - they were
  already UTC.
- **The CLI now exits 1** on a bad flag instead of 0. A script written as
  `quote -source=bogus && next-step` will now stop where it used to continue.

## License

MIT License - see LICENSE for more details
