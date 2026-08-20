# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

go-quote is a Go library and CLI tool for downloading historical price quotes. It supports:
- Tiingo API (stocks, daily/intraday data) - requires TIINGO_API_TOKEN
- Tiingo Crypto API (cryptocurrency data)
- Coinbase API (cryptocurrency exchange data)
- Binance API (cryptocurrency exchange data, via data-api.binance.vision)

The project has no external dependencies beyond the Go standard library.

## Build and Test

```bash
# Build the CLI
go build ./quote

# Run tests
go test

# Install CLI globally
go install github.com/markcheno/go-quote/quote@latest

# Run CLI
quote -help
```

## Architecture

### Core Components

The library is one package (`quote`) at the repo root, split across topic files:

| File | Contents |
| --- | --- |
| `quote.go` | `Quote`/`Quotes`/`Period` types, constants, `Log`, `Delay`, helpers |
| `client.go` | `Client`, `DefaultClient`, shared HTTP transport, `RetryPolicy`, `HTTPError` |
| `provider.go` | `Provider` interface, registry, the three built-in sources |
| `period.go` | `ParsePeriod` and the period vocabulary |
| `format.go` | `Format`, `ParseFormat`, `Encode`/`WriteFile` |
| `encoding.go` | CSV/JSON/Highstock/Amibroker encoders and parsers |
| `tiingo.go` / `coinbase.go` / `binance.go` | data sources |
| `market.go` / `ftp.go` | `marketSources` table, symbol lists; anonymous FTP |
| `update.go` | `UpdateFileTiingo` - in-place CSV updates with backfill |
| `errors.go` / `fetchall.go` | `SymbolNotFoundError`; `FetchAll` |
| `docs/adding-a-provider.md` | checklist for adding a data source |

**Preferred API**: construct a `Client` and use `Client.Provider(name)` plus
`Provider.Fetch(ctx, Request)`. The older package-level functions
(`NewQuoteFromTiingo`, the `Write*` methods, `Delay`, `Log`) still work and
delegate to `DefaultClient`, but are marked `// Deprecated:`. Adding an output
format means extending `Format`. Adding a data source means implementing
`Provider` and calling `RegisterProvider` - that covers CLI dispatch and period
validation, but not the base-URL test seam, `Quote.Precision`, UTC timestamps,
or the three tests that enumerate sources. See `docs/adding-a-provider.md`.

**quote/main.go** - CLI wrapper (~425 lines):
- Flag parsing for all options (years, period, source, format, etc.)
- Symbol resolution from files, markets, or arguments (supports wildcards in -infile)
- Two output modes: individual files per symbol or all-in-one file (-all=true)
- Update mode entry point

### Data Flow

1. **Symbol Resolution**: CLI resolves symbols from -infile (with wildcard support), -markets flag, or command args
2. **Fetch**: Library makes HTTP requests to Tiingo/Coinbase with proper headers and rate limiting
3. **Transform**: API JSON responses are parsed into Quote structs with OHLCV data
4. **Output**: Data written as CSV (default), JSON, Highstock format, or Amibroker format

### Update Mode Architecture

The `-update` flag enables incremental CSV updates for Tiingo data (update.go):

- **Single-symbol CSVs**: Infers symbol from filename (e.g., spy.csv → SPY)
- **Multi-symbol CSVs**: Preserves original symbol order from input file
- **Backfill window**: `-backfill-days` (default 10) rewrites overlap period to catch adjustments
- **Corporate action detection**: Detects splits/dividends in backfill window; if `-full-redownload-on-ca` is set, re-fetches entire history for that symbol
- **Concurrency**: `-concurrency` controls parallel symbol fetching (multi-symbol only)
- **Rate limiting**: Respects global `Delay` variable (set via `-delay` flag) using ticker-based limiter across all workers

Implementation uses two-pass approach:
1. First pass: Scan file to determine date ranges and symbol order
2. Second pass: Rewrite .tmp file with preserved data before cutoff + new/updated data from API

## Key Implementation Details

### Rate Limiting
- `Client.Delay` is a real `time.Duration` and is the preferred control
- The deprecated global `quote.Delay` holds a raw millisecond count despite its
  `time.Duration` type (`Delay = 100` means 100ms); `Client.rateLimit` converts
- CLI sets the global via `-delay` flag (default 100ms)
- Update mode uses `time.Ticker` for global rate limiting across concurrent workers
- `Client.Retry` adds backoff for 429/5xx; disabled by default

### API Interactions
- **Tiingo**: Uses Authorization header with token, supports date ranges and resample frequencies
- **Coinbase**: Public API, fetches in 200-bar chunks with pagination
- **Binance**: Public API, 1000-bar pages. The `limit` is clamped silently, so
  paging resumes from the last bar returned, never from a computed window.
  Uses `data-api.binance.vision`; `api.binance.com` returns 451 outside
  eligible regions. A bad symbol is a 400 with `code:-1121`, not a 404
- **NASDAQ API**: Fetches market screeners and symbol lists via JSON API
- **FTP**: ETF list via anonymous FTP to ftp.nasdaqtrader.com

### Testing Strategy
- quote_test.go: CSV/JSON encoders, parsers, period/format parsing, provider registry
- client_test.go: provider HTTP paths via `httptest.Server`, using the unexported
  `tiingoBase`/`coinbaseBase`/`nasdaqBase`/`binanceBase` fields on `Client` to
  redirect requests
- Update tests use `tiingoFetch` variable indirection for mocking
- Tests use `t.TempDir()` for isolated file operations
- Compatibility gate: `apidiff` against master must report no incompatible changes

### Period Handling
`ParsePeriod` converts user input to a `Period`; each `Provider.Periods()`
declares what that source supports, and an unsupported period is an error
rather than a silent fallback to daily:
- Tiingo daily: d, w, m
- Tiingo crypto: 1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h, 6h, 8h, 12h, d
- Coinbase: 1m, 5m, 15m, 30m, 1h, d, w (mapped to granularity in seconds)
- Binance: every period, including 3d - the only source that supports it

### Markets
`marketSources` (market.go) is the authoritative table mapping a market name to
its URL builder and response parser; `ValidMarkets` is the documented list and
`TestMarketTableMatchesValidMarkets` keeps the two in step. Adding a market
means one table entry, not a switch arm plus a dispatch branch - the drift
between those two is what made `etf` unreachable. URLs are built from
`c.nasdaqURL()`/`c.tiingoURL()`/`c.coinbaseURL()`/`c.binanceURL()`, so market
lists can be pointed at an `httptest.Server`.

`ValidMarkets` is an array, so its type changes whenever a market is added; it
is deprecated in favor of `Markets() []string`.

### Timezones
All times this package produces are UTC. `time.Unix` and `time.Now` both
return local times, so any new call site must add `.UTC()` - the CSV format
carries no zone, so a local wall clock written out and parsed back as UTC
shifts the instant by the local offset.

### CSV Format
Single-symbol: `datetime,open,high,low,close,volume`
Multi-symbol: `symbol,datetime,open,high,low,close,volume`
Date format: `2006-01-02 15:04` (time is always 00:00 for daily data)

## Development Notes

- Go version: 1.24+ (per go.mod); `strings.SplitSeq` sets that floor
- No external dependencies
- All HTTP clients use 10-second timeout (`ClientTimeout` constant)
- NASDAQ API requests send a fixed `markcheno/go-quote` user agent
- Precision: data sources stamp `Quote.Precision` (`PrecisionEquity`=2,
  `PrecisionCrypto`=8); CSV parsers infer it from the decimals present in the
  file. `getPrecision`'s symbol-name guess is only a fallback for hand-built
  Quotes - it misreads EUR/GBP-quoted crypto pairs as 2-decimal equities
