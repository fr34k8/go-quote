package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// ValidMarkets list of markets that can be downloaded
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
}

// ValidMarket - validate market string
func ValidMarket(market string) bool {
	for _, v := range ValidMarkets {
		if v == market {
			return true
		}
	}
	return false
}

// MarketRequiresToken - reports whether a market needs TIINGO_API_TOKEN set.
// ValidMarket answers only "is this a known market name"; the credential
// check is separate so callers can report it themselves.
func MarketRequiresToken(market string) bool {
	return strings.HasPrefix(market, "tiingo")
}

// marketListUserAgent identifies go-quote to the NASDAQ API.
const marketListUserAgent = "markcheno/go-quote"

// NewMarketList - download a list of market symbols to an array of strings
func NewMarketList(market string) ([]string, error) {

	var symbols []string
	if !ValidMarket(market) {
		return symbols, fmt.Errorf("invalid market")
	}

	if MarketRequiresToken(market) && os.Getenv("TIINGO_API_TOKEN") == "" {
		return symbols, fmt.Errorf("market %q requires TIINGO_API_TOKEN to be set", market)
	}

	// etf comes from the NASDAQ symbol directory over FTP, not a JSON API.
	if market == "etf" {
		return NewEtfList()
	}

	var url string
	switch market {
	case "nasdaq":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&exchange=NASDAQ"
	case "nasdaq100":
		url = "https://api.nasdaq.com/api/quote/list-type/nasdaq100"
	case "amex":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&exchange=AMEX"
	case "nyse":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&exchange=NYSE"
	case "megacap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=mega"
	case "largecap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=large"
	case "midcap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=mid"
	case "smallcap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=small"
	case "microcap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=micro"
	case "nanocap":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&marketcap=nano"
	case "telecommunications":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=telecommunications"
	case "health_care":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=health_care"
	case "finance":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=finance"
	case "real_estate":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=real_estate"
	case "consumer_discretionary":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=consumer_discretionary"
	case "consumer_staples":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=consumer_staples"
	case "industrials":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=industrials"
	case "basic_materials":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=basic_materials"
	case "energy":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=energy"
	case "utilities":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=utilities"
	case "technology":
		url = "https://api.nasdaq.com/api/screener/stocks?tableonly=true&offset=0&download=true&sector=technology"
	case "tiingo-btc":
		url = fmt.Sprintf("https://api.tiingo.com/tiingo/crypto?token=%s", os.Getenv("TIINGO_API_TOKEN"))
	case "tiingo-eth":
		url = fmt.Sprintf("https://api.tiingo.com/tiingo/crypto?token=%s", os.Getenv("TIINGO_API_TOKEN"))
	case "tiingo-usd":
		url = fmt.Sprintf("https://api.tiingo.com/tiingo/crypto?token=%s", os.Getenv("TIINGO_API_TOKEN"))
	case "coinbase":
		url = "https://api.exchange.coinbase.com/products"
	default:
		// ValidMarkets and this switch must stay in sync; without this arm a
		// missing case silently yields an empty url and an obscure
		// "unsupported protocol scheme" from the http client.
		return symbols, fmt.Errorf("no source configured for market %q", market)
	}

	// This call site previously used a bare &http.Client{} with no timeout at
	// all, so a hung NASDAQ response blocked forever.
	header := http.Header{}
	header.Set("User-Agent", marketListUserAgent)
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/json; charset=utf-8")

	body, err := DefaultClient.get(context.Background(), url, header)
	if err != nil {
		return symbols, err
	}
	newStr := string(body)

	if strings.HasPrefix(market, "tiingo") {
		return getTiingoCryptoMarket(market, newStr)
	}

	if strings.HasPrefix(market, "coinbase") {
		return getCoinbaseMarket(market, newStr)
	}

	if market == "nasdaq100" {
		return getNasdaq100Market(market, newStr)
	}

	return getNasdaqMarket(market, newStr)

}

func getTiingoCryptoMarket(market, rawdata string) ([]string, error) {

	type Symbol struct {
		Ticker        string `json:"ticker"`
		Name          string `json:"name"`
		BaseCurrency  string `json:"baseCurrency"`
		QuoteCurrency string `json:"quoteCurrency"`
	}

	var markets []Symbol

	err := json.Unmarshal([]byte(rawdata), &markets)
	if err != nil {
		fmt.Println(err)
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

	return symbols, err
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

	err := json.Unmarshal([]byte(rawdata), &markets)
	if err != nil {
		fmt.Println(err)
	}

	var symbols []string
	for _, mkt := range markets {
		if !mkt.TradingDisabled {
			symbols = append(symbols, mkt.ID)
		}
	}

	sort.Strings(symbols)

	return symbols, err
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
