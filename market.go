package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Market symbol lists from the NASDAQ API, Tiingo, Coinbase, and FTP.

// NewEtfList - download a list of etf symbols to an array of strings
func NewEtfList() ([]string, error) {

	var symbols []string

	buf, err := getAnonFTP("ftp.nasdaqtrader.com", "21", "symboldirectory", "otherlisted.txt")
	if err != nil {
		Log.Println(err)
		return symbols, err
	}

	for line := range strings.SplitSeq(string(buf), "\n") {
		// ACT Symbol|Security Name|Exchange|CQS Symbol|ETF|Round Lot Size|Test Issue|NASDAQ Symbol
		cols := strings.Split(line, "|")
		if len(cols) > 5 && cols[4] == "Y" && cols[6] == "N" {
			symbols = append(symbols, strings.ToLower(cols[0]))
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// NewEtfFile - download a list of etf symbols to a file
func NewEtfFile(filename string) error {
	if filename == "" {
		filename = "etf.txt"
	}
	etfs, err := NewEtfList()
	if err != nil {
		return err
	}
	ba := []byte(strings.Join(etfs, "\n"))
	return os.WriteFile(filename, ba, 0644)
}

// marketSource describes where one market's symbol list comes from and how to
// read the response.
//
// This used to be a hand-written switch of 25 near-identical URL cases plus a
// separate HasPrefix dispatch chain, which is how "etf" became a market that
// validated but could never be fetched: the two halves drifted apart. A table
// keeps the URL and its parser in the same place, and TestMarketTableMatchesValidMarkets
// keeps the table and ValidMarkets in step.
type marketSource struct {
	// list bypasses the HTTP path entirely. Only etf uses it: the NASDAQ
	// symbol directory is served over anonymous FTP.
	list func() ([]string, error)

	// url builds the request URL against a client, so tests can redirect it.
	url func(c *Client) string

	// parse turns the response body into a symbol list.
	parse func(market, rawdata string) ([]string, error)

	// requiresToken reports whether TIINGO_API_TOKEN must be set.
	requiresToken bool
}

// nasdaqScreener builds a market backed by one NASDAQ stock-screener filter.
// Twenty-one of the markets differ only by this single query parameter.
func nasdaqScreener(param, value string) marketSource {
	return marketSource{
		url: func(c *Client) string {
			return fmt.Sprintf("%s/api/screener/stocks?tableonly=true&offset=0&download=true&%s=%s",
				c.nasdaqURL(), param, value)
		},
		parse: getNasdaqMarket,
	}
}

// tiingoCryptoMarket builds a market backed by Tiingo's crypto ticker list. All three
// share one endpoint; the market name selects the quote currency when parsing.
func tiingoCryptoMarket() marketSource {
	return marketSource{
		url: func(c *Client) string {
			return fmt.Sprintf("%s/tiingo/crypto?token=%s",
				c.tiingoURL(), url.QueryEscape(os.Getenv("TIINGO_API_TOKEN")))
		},
		parse:         getTiingoCryptoMarket,
		requiresToken: true,
	}
}

// binanceExchange builds a market backed by Binance's exchangeInfo. As with
// Tiingo, one endpoint serves every quote asset.
func binanceExchange() marketSource {
	return marketSource{
		url:   func(c *Client) string { return c.binanceURL() + "/api/v3/exchangeInfo" },
		parse: getBinanceMarket,
	}
}

// marketSources is the authoritative set of markets.
var marketSources = map[string]marketSource{
	"etf":                    {list: NewEtfList},
	"nasdaq":                 nasdaqScreener("exchange", "NASDAQ"),
	"amex":                   nasdaqScreener("exchange", "AMEX"),
	"nyse":                   nasdaqScreener("exchange", "NYSE"),
	"megacap":                nasdaqScreener("marketcap", "mega"),
	"largecap":               nasdaqScreener("marketcap", "large"),
	"midcap":                 nasdaqScreener("marketcap", "mid"),
	"smallcap":               nasdaqScreener("marketcap", "small"),
	"microcap":               nasdaqScreener("marketcap", "micro"),
	"nanocap":                nasdaqScreener("marketcap", "nano"),
	"telecommunications":     nasdaqScreener("sector", "telecommunications"),
	"health_care":            nasdaqScreener("sector", "health_care"),
	"finance":                nasdaqScreener("sector", "finance"),
	"real_estate":            nasdaqScreener("sector", "real_estate"),
	"consumer_discretionary": nasdaqScreener("sector", "consumer_discretionary"),
	"consumer_staples":       nasdaqScreener("sector", "consumer_staples"),
	"industrials":            nasdaqScreener("sector", "industrials"),
	"basic_materials":        nasdaqScreener("sector", "basic_materials"),
	"energy":                 nasdaqScreener("sector", "energy"),
	"utilities":              nasdaqScreener("sector", "utilities"),
	"technology":             nasdaqScreener("sector", "technology"),

	"nasdaq100": {
		url:   func(c *Client) string { return c.nasdaqURL() + "/api/quote/list-type/nasdaq100" },
		parse: getNasdaq100Market,
	},
	"coinbase": {
		url:   func(c *Client) string { return c.coinbaseURL() + "/products" },
		parse: getCoinbaseMarket,
	},

	"tiingo-btc": tiingoCryptoMarket(),
	"tiingo-eth": tiingoCryptoMarket(),
	"tiingo-usd": tiingoCryptoMarket(),

	"binance-usdt": binanceExchange(),
	"binance-usdc": binanceExchange(),
	"binance-btc":  binanceExchange(),
	"binance-eth":  binanceExchange(),
}

// ValidMarkets list of markets that can be downloaded
//
// Deprecated: use Markets, which returns a slice. The length of an array is
// part of its type, so every market added here changes the type of this
// variable.
var ValidMarkets = [...]string{
	"etf",
	"nasdaq",
	"nasdaq100",
	"amex",
	"nyse",
	"megacap",
	"largecap",
	"midcap",
	"smallcap",
	"microcap",
	"nanocap",
	"telecommunications",
	"health_care",
	"finance",
	"real_estate",
	"consumer_discretionary",
	"consumer_staples",
	"industrials",
	"basic_materials",
	"energy",
	"utilities",
	"technology",
	"tiingo-btc",
	"tiingo-eth",
	"tiingo-usd",
	"coinbase",
	"binance-usdt",
	"binance-usdc",
	"binance-btc",
	"binance-eth",
}

// Markets lists the downloadable markets in sorted order.
func Markets() []string {
	names := make([]string, 0, len(marketSources))
	for name := range marketSources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidMarket - validate market string
func ValidMarket(market string) bool {
	_, ok := marketSources[market]
	return ok
}

// MarketRequiresToken - reports whether a market needs TIINGO_API_TOKEN set.
// ValidMarket answers only "is this a known market name"; the credential
// check is separate so callers can report it themselves.
func MarketRequiresToken(market string) bool {
	return marketSources[market].requiresToken
}

// marketListUserAgent identifies go-quote to the NASDAQ API.
const marketListUserAgent = "markcheno/go-quote"

// MarketList downloads the list of symbols for a market.
func (c *Client) MarketList(ctx context.Context, market string) ([]string, error) {
	src, ok := marketSources[market]
	if !ok {
		return nil, fmt.Errorf("invalid market %q, must be one of: %s", market, strings.Join(Markets(), ", "))
	}

	if src.requiresToken && os.Getenv("TIINGO_API_TOKEN") == "" {
		return nil, fmt.Errorf("market %q requires TIINGO_API_TOKEN to be set", market)
	}

	// etf comes from the NASDAQ symbol directory over FTP, not a JSON API.
	if src.list != nil {
		return src.list()
	}

	// This call site previously used a bare &http.Client{} with no timeout at
	// all, so a hung NASDAQ response blocked forever.
	header := http.Header{}
	header.Set("User-Agent", marketListUserAgent)
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/json; charset=utf-8")

	body, err := c.get(ctx, src.url(c), header)
	if err != nil {
		return nil, err
	}
	return src.parse(market, string(body))
}

// NewMarketList - download a list of market symbols to an array of strings
//
// Deprecated: use Client.MarketList, which takes a context.Context.
func NewMarketList(market string) ([]string, error) {
	return DefaultClient.MarketList(context.Background(), market)
}

func getTiingoCryptoMarket(market, rawdata string) ([]string, error) {

	type Symbol struct {
		Ticker        string `json:"ticker"`
		Name          string `json:"name"`
		BaseCurrency  string `json:"baseCurrency"`
		QuoteCurrency string `json:"quoteCurrency"`
	}

	var markets []Symbol

	if err := json.Unmarshal([]byte(rawdata), &markets); err != nil {
		return nil, fmt.Errorf("parsing %s market json: %w", market, err)
	}

	var symbols []string
	for _, mkt := range markets {
		if strings.HasSuffix(market, "btc") && mkt.QuoteCurrency == "btc" {
			symbols = append(symbols, mkt.Ticker)
		} else if strings.HasSuffix(market, "eth") && mkt.QuoteCurrency == "eth" {
			symbols = append(symbols, mkt.Ticker)
		} else if strings.HasSuffix(market, "usd") && mkt.QuoteCurrency == "usd" {
			symbols = append(symbols, mkt.Ticker)
		}
	}

	sort.Strings(symbols)
	return symbols, nil
}

func getNasdaqMarket(market, rawdata string) ([]string, error) {

	// https://www.nasdaq.com/market-activity/stocks/screener

	type Headers struct {
		Symbol    string `json:"symbol"`
		Name      string `json:"name"`
		LastSale  string `json:"lastsale"`
		NetChange string `json:"netchange"`
		PctChange string `json:"pctchange"`
		MarketCap string `json:"marketCap"`
	}

	type Row struct {
		Symbol    string `json:"symbol"`
		Name      string `json:"name"`
		LastSale  string `json:"lastsale"`
		NetChange string `json:"netchange"`
		PctChange string `json:"pctchange"`
		MarketCap string `json:"marketCap"`
		URL       string `json:"url"`
	}

	type Table struct {
		AsOf    *string `json:"asOf"`
		Headers Headers `json:"headers"`
		Rows    []Row   `json:"rows"`
	}

	type Status struct {
		RCode            int     `json:"rCode"`
		BCodeMessage     *string `json:"bCodeMessage"`
		DeveloperMessage *string `json:"developerMessage"`
	}

	type ApiResponse struct {
		Data    Table   `json:"data"`
		Message *string `json:"message"`
		Status  Status  `json:"status"`
	}

	// Unmarshal the JSON into our structs
	var apiResponse ApiResponse
	err := json.Unmarshal([]byte(rawdata), &apiResponse)
	if err != nil {
		return nil, fmt.Errorf("parsing %s market json: %w", market, err)
	}

	var symbols []string
	for _, row := range apiResponse.Data.Rows {
		symbols = append(symbols, strings.ToLower(row.Symbol))
	}

	sort.Strings(symbols)

	return symbols, nil
}

func getNasdaq100Market(market, rawdata string) ([]string, error) {

	// https://api.nasdaq.com/api/quote/list-type/nasdaq100

	type Headers struct {
		Symbol    string `json:"symbol"`
		Name      string `json:"companyName"`
		MarketCap string `json:"marketCap"`
		LastSale  string `json:"lastSalePrice"`
		NetChange string `json:"netChange"`
		PctChange string `json:"percentageChange"`
	}

	type Row struct {
		Symbol        string `json:"symbol"`
		Sector        string `json:"sector"`
		Name          string `json:"companyName"`
		MarketCap     string `json:"marketCap"`
		LastSalePrice string `json:"lastSalePrice"`
		NetChange     string `json:"netChange"`
		PctChange     string `json:"percentageChange"`
		Delta         string `json:"deltaIndicator"`
	}

	type Table struct {
		AsOf    *string `json:"asOf"`
		Headers Headers `json:"headers"`
		Rows    []Row   `json:"rows"`
	}

	type Data struct {
		TotalRecords int    `json:"totalrecords"`
		Limit        int    `json:"limit"`
		Offset       int    `json:"offset"`
		Date         string `json:"date"`
		Data         Table  `json:"data"`
	}

	type Status struct {
		RCode            int     `json:"rCode"`
		BCodeMessage     *string `json:"bCodeMessage"`
		DeveloperMessage *string `json:"developerMessage"`
	}

	type ApiResponse struct {
		Data    Data    `json:"data"`
		Message *string `json:"message"`
		Status  Status  `json:"status"`
	}

	// Unmarshal the JSON into our structs
	var apiResponse ApiResponse
	err := json.Unmarshal([]byte(rawdata), &apiResponse)
	if err != nil {
		return nil, fmt.Errorf("parsing %s market json: %w", market, err)
	}

	var symbols []string
	for _, row := range apiResponse.Data.Data.Rows {
		symbols = append(symbols, strings.ToLower(row.Symbol))
	}

	sort.Strings(symbols)

	return symbols, nil
}

func getCoinbaseMarket(market, rawdata string) ([]string, error) {

	type Symbol struct {
		ID                     string `json:"id"`
		BaseCurrency           string `json:"base_currency"`
		QuoteCurrency          string `json:"quote_currency"`
		QuoteIncrement         string `json:"quote_increment"`
		BaseIncrement          string `json:"base_increment"`
		DisplayName            string `json:"display_name"`
		MinMarketFunds         string `json:"min_market_funds"`
		MarginEnabled          bool   `json:"margin_enabled"`
		PostOnly               bool   `json:"post_only"`
		LimitOnly              bool   `json:"limit_only"`
		CancelOnly             bool   `json:"cancel_only"`
		Status                 string `json:"status"`
		StatusMessage          string `json:"status_message"`
		TradingDisabled        bool   `json:"trading_disabled"`
		FxStablecoin           bool   `json:"fx_stablecoin"`
		MaxSlippagePercentage  string `json:"max_slippage_percentage"`
		AuctionMode            bool   `json:"auction_mode"`
		HighBidLimitPercentage string `json:"high_bid_limit_percentage"`
	}

	var markets []Symbol

	if err := json.Unmarshal([]byte(rawdata), &markets); err != nil {
		return nil, fmt.Errorf("parsing %s market json: %w", market, err)
	}

	var symbols []string
	for _, mkt := range markets {
		if !mkt.TradingDisabled {
			symbols = append(symbols, mkt.ID)
		}
	}

	sort.Strings(symbols)

	return symbols, nil
}

// getBinanceMarket selects the pairs quoted in one asset from Binance's
// exchangeInfo, e.g. "binance-usdt" keeps every *USDT pair.
func getBinanceMarket(market, rawdata string) ([]string, error) {

	type Symbol struct {
		Symbol     string `json:"symbol"`
		Status     string `json:"status"`
		BaseAsset  string `json:"baseAsset"`
		QuoteAsset string `json:"quoteAsset"`
	}

	type exchangeInfo struct {
		Symbols []Symbol `json:"symbols"`
	}

	var info exchangeInfo
	if err := json.Unmarshal([]byte(rawdata), &info); err != nil {
		return nil, fmt.Errorf("parsing %s market json: %w", market, err)
	}

	quoteAsset := strings.ToUpper(strings.TrimPrefix(market, "binance-"))

	var symbols []string
	for _, mkt := range info.Symbols {
		// The removed 2024 implementation did not check Status, so delisted
		// and halted pairs came back looking tradeable. Only about a third of
		// the symbols exchangeInfo returns are actually TRADING.
		if mkt.Status == "TRADING" && mkt.QuoteAsset == quoteAsset {
			symbols = append(symbols, mkt.Symbol)
		}
	}

	sort.Strings(symbols)

	return symbols, nil
}

// NewMarketFile - download a list of market symbols to a file
func NewMarketFile(market, filename string) error {
	if !ValidMarket(market) {
		return fmt.Errorf("invalid market")
	}
	// default filename
	if filename == "" {
		filename = market + ".txt"
	}
	syms, err := NewMarketList(market)
	if err != nil {
		return err
	}

	// Trim whitespace from each symbol
	for i := range syms {
		syms[i] = strings.TrimSpace(syms[i])
	}

	ba := []byte(strings.Join(syms, "\n"))
	return os.WriteFile(filename, ba, 0644)
}

// NewSymbolsFromFile - read symbols from a file
func NewSymbolsFromFile(filename string) ([]string, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return []string{}, err
	}

	a := strings.Split(strings.ToLower(string(raw)), "\n")

	return deleteEmpty(a), nil
}

// delete empty strings from a string array
func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}
