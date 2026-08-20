package quote

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Request describes one symbol fetch.
type Request struct {
	Symbol string
	From   time.Time
	To     time.Time
	Period Period
	// Token overrides Client.Token for sources that need credentials.
	Token string
}

// Provider fetches quotes from a single data source.
//
// Adding a source means implementing this and calling RegisterProvider; it no
// longer requires editing parallel if/else chains in the CLI.
type Provider interface {
	// Name is the source identifier, e.g. "tiingo".
	Name() string
	// Periods lists the periods this source supports.
	Periods() []Period
	// Fetch returns the bars for one symbol.
	Fetch(ctx context.Context, req Request) (Quote, error)
}

// providerFactory binds a provider to a Client.
type providerFactory func(*Client) Provider

var providerRegistry = map[string]providerFactory{}

// RegisterProvider adds a named source to the registry, replacing any existing
// entry with the same name.
func RegisterProvider(name string, f providerFactory) {
	providerRegistry[name] = f
}

// ProviderNames lists the registered sources in sorted order.
func ProviderNames() []string {
	names := make([]string, 0, len(providerRegistry))
	for name := range providerRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Provider returns the named source bound to c.
func (c *Client) Provider(name string) (Provider, error) {
	f, ok := providerRegistry[name]
	if !ok {
		return nil, fmt.Errorf("invalid source %q, must be one of: %s",
			name, strings.Join(ProviderNames(), ", "))
	}
	return f(c), nil
}

// CheckPeriod reports whether p supports period.
func CheckPeriod(p Provider, period Period) error {
	if slices.Contains(p.Periods(), period) {
		return nil
	}
	strs := make([]string, 0, len(p.Periods()))
	for _, v := range p.Periods() {
		strs = append(strs, PeriodString(v))
	}
	return fmt.Errorf("invalid period %q for source %q, must be one of: %s",
		PeriodString(period), p.Name(), strings.Join(strs, ", "))
}

// FetchSymbols fetches every symbol through p, pausing Client.Delay between
// requests and skipping symbols that fail.
func (c *Client) FetchSymbols(ctx context.Context, p Provider, symbols []string, req Request) (Quotes, error) {
	if err := CheckPeriod(p, req.Period); err != nil {
		return Quotes{}, err
	}
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		r := req
		r.Symbol = symbol
		return p.Fetch(ctx, r)
	})
}

// --- built-in providers ---

type tiingoProvider struct{ c *Client }

func (p tiingoProvider) Name() string { return "tiingo" }

func (p tiingoProvider) Periods() []Period {
	return []Period{Daily, Weekly, Monthly}
}

func (p tiingoProvider) Fetch(ctx context.Context, req Request) (Quote, error) {
	return p.c.TiingoDaily(ctx, req.Symbol, req.From, req.To, req.Period, req.Token)
}

type tiingoCryptoProvider struct{ c *Client }

func (p tiingoCryptoProvider) Name() string { return "tiingo-crypto" }

func (p tiingoCryptoProvider) Periods() []Period {
	return []Period{Min1, Min3, Min5, Min15, Min30, Min60, Hour2, Hour4, Hour6, Hour8, Hour12, Daily}
}

func (p tiingoCryptoProvider) Fetch(ctx context.Context, req Request) (Quote, error) {
	return p.c.TiingoCrypto(ctx, req.Symbol, req.From, req.To, req.Period, req.Token)
}

type coinbaseProvider struct{ c *Client }

func (p coinbaseProvider) Name() string { return "coinbase" }

func (p coinbaseProvider) Periods() []Period {
	return []Period{Min1, Min5, Min15, Min30, Min60, Daily, Weekly}
}

func (p coinbaseProvider) Fetch(ctx context.Context, req Request) (Quote, error) {
	return p.c.Coinbase(ctx, req.Symbol, req.From, req.To, req.Period)
}

type binanceProvider struct{ c *Client }

func (p binanceProvider) Name() string { return "binance" }

// Periods reports every period this package defines: Binance serves all of
// them, and is the only source here that covers Day3.
func (p binanceProvider) Periods() []Period {
	return []Period{Min1, Min3, Min5, Min15, Min30, Min60, Hour2, Hour4, Hour6,
		Hour8, Hour12, Daily, Day3, Weekly, Monthly}
}

func (p binanceProvider) Fetch(ctx context.Context, req Request) (Quote, error) {
	return p.c.Binance(ctx, req.Symbol, req.From, req.To, req.Period)
}

func init() {
	RegisterProvider("tiingo", func(c *Client) Provider { return tiingoProvider{c} })
	RegisterProvider("tiingo-crypto", func(c *Client) Provider { return tiingoCryptoProvider{c} })
	RegisterProvider("coinbase", func(c *Client) Provider { return coinbaseProvider{c} })
	RegisterProvider("binance", func(c *Client) Provider { return binanceProvider{c} })
}
