# Adding a data source

A data source is a `Provider`. The registry handles CLI dispatch and period
validation for you, but four other things still need attention, and two of them
fail silently — corrupting data rather than erroring — if you skip them.

`binance.go` is the worked example throughout; it was added by following this
list.

```go
type Provider interface {
	Name() string                                      // "binance"
	Periods() []Period                                 // what this source can serve
	Fetch(ctx context.Context, req Request) (Quote, error)
}
```

## From outside the package

If you only need a source for your own program, you do not have to modify
go-quote at all. Register it and the CLI machinery treats it like a built-in:

```go
type myProvider struct{ c *quote.Client }

func (p myProvider) Name() string            { return "mysource" }
func (p myProvider) Periods() []quote.Period { return []quote.Period{quote.Daily} }
func (p myProvider) Fetch(ctx context.Context, req quote.Request) (quote.Quote, error) {
	bars, err := p.fetchFromMyAPI(ctx, req)
	if err != nil {
		return quote.NewQuote("", 0), err
	}

	q := quote.NewQuote(req.Symbol, len(bars))
	q.Precision = quote.PrecisionEquity
	for i, b := range bars {
		q.Date[i] = b.Time.UTC()
		q.Open[i], q.High[i], q.Low[i] = b.Open, b.High, b.Low
		q.Close[i], q.Volume[i] = b.Close, b.Volume
	}
	return q, nil
}

quote.RegisterProvider("mysource", func(c *quote.Client) quote.Provider {
	return myProvider{c}
})
```

`Client.Provider("mysource")` now resolves, and `CheckPeriod` rejects anything
`Periods()` does not list.

The rest of this document is for adding a source to the package itself.

## 1. The base URL seam

Three edits in [`client.go`](../client.go), all following the shape already
there:

```go
const defaultBinanceBaseURL = "https://data-api.binance.vision"   // const block

type Client struct {
	// ...
	binanceBase string                                             // with the other base fields
}

func (c *Client) binanceURL() string {                             // nil-receiver safe
	if c != nil && c.binanceBase != "" {
		return c.binanceBase
	}
	return defaultBinanceBaseURL
}
```

Build every URL from the accessor, never from the constant. That is the only
thing that lets a test point the source at an `httptest.Server` — and it is why
market lists had no HTTP coverage for years: they used hardcoded absolute
strings, so `nasdaqURL()` sat unused.

Then add the field to `newTestClient` in [`client_test.go`](../client_test.go).

## 2. The period mapping

One function, exhaustive switch, no `default`:

```go
func binanceInterval(period Period) (string, error) {
	switch period {
	case Min1:
		return "1m", nil
	// ...
	case Daily, "":
		return "1d", nil
	}
	return "", fmt.Errorf("period %q is not supported by binance", period)
}
```

A `default` arm that returns the daily value is the bug this shape exists to
prevent. Coinbase, tiingo-crypto and the removed binance implementation all had
one, so asking for `-period=6h` quietly returned daily bars — data that looks
plausible and is wrong. `Periods()` must list exactly what this function
accepts; `TestProviderPeriodValidation` and a `…RejectsUnsupported` test pin
both halves.

## 3. The fetch

Use `c.get(ctx, url, header)`. It centralizes status checking, `Retry-After`
backoff for 429/5xx, body closing, and the request-construction error that five
call sites used to discard.

Paging, if the API needs it, should **advance from the data you received, not
from the window you asked for**. Binance clamps `limit` to 1000 without saying
so — request 1500 and you get 1000 bars and no error — so a computed step skips
every bar a short page did not cover. `Client.Binance` resumes from the last
returned bar's timestamp plus one millisecond, and
`TestClientBinancePagingAcrossFullPage` fails if that is changed back to
arithmetic.

Not every API reports a missing symbol the same way. Tiingo returns 404;
Binance returns **400 with `{"code":-1121}` in the body**, so identifying it
needs the body, not just the status. Either way, return `*SymbolNotFoundError`:
`FetchAll` skips those symbols and keeps going, and update mode treats them as
informational rather than fatal.

## 4. Two things that fail silently

Both were real bugs in this repository. Neither produces an error — you get a
file full of plausible, wrong numbers.

**Stamp `Quote.Precision`.** Without it, formatting falls back to
`getPrecision`, which guesses from the symbol name by looking for `BTC`, `ETH`
or `USD`. Coinbase quotes 50 of its pairs in EUR or GBP, which contain none of
those, so they were written as 2-decimal equities: a full day of `DOGE-EUR`
came out as `0.08,0.08,0.08,0.08`. Set `PrecisionEquity` or `PrecisionCrypto`
at fetch time and the heuristic never runs.

**Call `.UTC()` on every timestamp.** `time.Unix`, `time.UnixMilli` and
`time.Now` all return local times, and the CSV format carries no zone — so a
local wall clock written out and parsed back as UTC lands on a different
instant. Run the suite under `TZ=Asia/Tokyo` to check.

## 5. Tests you must *update*, not just add

Three existing tests know the set of sources and will fail or silently skip
your provider:

| Test | What to do |
| --- | --- |
| `TestProviderRegistry` | hardcodes the expected `ProviderNames()`; add yours (sorted) |
| `TestProvidersSetPrecision` | add a handler `case` and assert your precision |
| `TestAllSourcesProduceUTC` | add a handler `case` and a row to the table |

`TestProviderRegistry` failing is the first thing you will see after
registering. That is intentional — it is the reminder that the other two exist.

Worth adding: a fetch test over `httptest` asserting path, query, and parsed
values; a `…RejectsUnsupported` period test; a not-found test; and a paging test
where the stub returns **fewer bars than requested**.

## 6. Symbol lists (optional)

To publish a market list, add one entry to `marketSources` in
[`market.go`](../market.go) — a URL builder and a parser:

```go
"binance-usdt": binanceExchange(),
```

Add the same name to `ValidMarkets`, or `TestMarketTableMatchesValidMarkets`
will fail. Note that `ValidMarkets` is an array, so adding to it changes its
type; `Markets()` is the slice-returning replacement.

## 7. Docs

- `usage` in [`quote/main.go`](../quote/main.go) — the source list, and the
  market list if you added one
- the `-source` flag help string just below it
- `README.md`'s usage block — **regenerate it from the `usage` variable rather
  than editing by hand**; the two drifted before, leaving a documented
  `-output` flag that never existed
- `CLAUDE.md`'s per-source period list

## Known rough edges

Things the registry does *not* cover, so you do not lose time rediscovering
them:

- **Credentials.** The CLI's token check is
  `strings.HasPrefix(flags.source, "tiingo")` in `quote/main.go`. A source that
  needs a key is not covered, and one named `tiingo-*` is wrongly forced to have
  a Tiingo token. The fix when someone needs it is an *optional* interface
  checked by type assertion:

  ```go
  type tokenRequirer interface{ RequiresToken() bool }
  ```

  **Not** a new method on `Provider` — adding a method to an exported interface
  breaks every external implementer.

- **Update mode** (`-update`) is hardcoded to `source != "tiingo"`. New sources
  are locked out by name.

- **Symbol shape is coupled to the market by convention only.** `coinbase`
  yields `BTC-USD`, `tiingo-usd` yields `btcusd`, `binance-usdt` yields
  `BTCUSDT`. Nothing validates that the symbols you feed a source came from a
  matching market; a mismatch just produces per-symbol fetch errors.

## Checklist

- [ ] base URL const, `Client` field, `xxxURL()` accessor, `newTestClient` entry
- [ ] period mapper with no `default` arm
- [ ] `Client.Xxx` fetch via `c.get`, paging driven by received data
- [ ] `*SymbolNotFoundError` for unknown symbols
- [ ] `Quote.Precision` set
- [ ] every timestamp `.UTC()`
- [ ] `XxxSyms` via `c.FetchAll`
- [ ] provider adapter and `init()` registration
- [ ] `TestProviderRegistry`, `TestProvidersSetPrecision` and
      `TestAllSourcesProduceUTC` updated
- [ ] `gofmt`, `go vet`, `go test -race`, and `apidiff` reporting no
      incompatible changes
