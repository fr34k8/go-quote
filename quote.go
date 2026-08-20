/*
Package quote is a free quote downloader library and cli.

It downloads historical price quotes from Tiingo and Coinbase.

Copyright 2025 Mark Chenoweth
Licensed under terms of MIT license (see LICENSE)
*/
package quote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
		return time.Now()
	}
	t, _ := time.Parse("2006-01-02 15:04", dt+"0000-01-01 00:00"[len(dt):])
	return t
}

func getPrecision(symbol string) int {
	var precision int
	precision = 2
	if strings.Contains(strings.ToUpper(symbol), "BTC") ||
		strings.Contains(strings.ToUpper(symbol), "ETH") ||
		strings.Contains(strings.ToUpper(symbol), "USD") {
		precision = 8
	}
	return precision
}

// CSV - convert Quote structure to csv string
func (q Quote) CSV() string {

	precision := getPrecision(q.Symbol)

	var buffer bytes.Buffer
	buffer.WriteString("datetime,open,high,low,close,volume\n")
	for bar := range q.Close {
		str := fmt.Sprintf("%s,%.*f,%.*f,%.*f,%.*f,%.*f\n", q.Date[bar].Format("2006-01-02 15:04"),
			precision, q.Open[bar], precision, q.High[bar], precision, q.Low[bar], precision, q.Close[bar], precision, q.Volume[bar])
		buffer.WriteString(str)
	}
	return buffer.String()
}

// Highstock - convert Quote structure to Highstock json format
func (q Quote) Highstock() string {

	precision := getPrecision(q.Symbol)

	var buffer bytes.Buffer
	buffer.WriteString("[\n")
	for bar := range q.Close {
		comma := ","
		if bar == len(q.Close)-1 {
			comma = ""
		}
		str := fmt.Sprintf("[%d,%.*f,%.*f,%.*f,%.*f,%.*f]%s\n",
			q.Date[bar].UnixNano()/1000000, precision, q.Open[bar], precision, q.High[bar], precision, q.Low[bar], precision, q.Close[bar], precision, q.Volume[bar], comma)
		buffer.WriteString(str)

	}
	buffer.WriteString("]\n")
	return buffer.String()
}

// Amibroker - convert Quote structure to csv string
func (q Quote) Amibroker() string {

	precision := getPrecision(q.Symbol)

	var buffer bytes.Buffer
	buffer.WriteString("date,time,open,high,low,close,volume\n")
	for bar := range q.Close {
		str := fmt.Sprintf("%s,%s,%.*f,%.*f,%.*f,%.*f,%.*f\n", q.Date[bar].Format("2006-01-02"), q.Date[bar].Format("15:04"),
			precision, q.Open[bar], precision, q.High[bar], precision, q.Low[bar], precision, q.Close[bar], precision, q.Volume[bar])
		buffer.WriteString(str)
	}
	return buffer.String()
}

// WriteCSV - write Quote struct to csv file
//
// Deprecated: use Quote.WriteFile(filename, FormatCSV).
func (q Quote) WriteCSV(filename string) error {
	if filename == "" {
		if q.Symbol != "" {
			filename = q.Symbol + ".csv"
		} else {
			filename = "quote.csv"
		}
	}
	csv := q.CSV()
	return os.WriteFile(filename, []byte(csv), 0644)
}

// WriteAmibroker - write Quote struct to csv file
//
// Deprecated: use Quote.WriteFile(filename, FormatAmibroker).
func (q Quote) WriteAmibroker(filename string) error {
	if filename == "" {
		if q.Symbol != "" {
			filename = q.Symbol + ".csv"
		} else {
			filename = "quote.csv"
		}
	}
	csv := q.Amibroker()
	return os.WriteFile(filename, []byte(csv), 0644)
}

// WriteHighstock - write Quote struct to Highstock json format
//
// Deprecated: use Quote.WriteFile(filename, FormatHighstock).
func (q Quote) WriteHighstock(filename string) error {
	if filename == "" {
		if q.Symbol != "" {
			filename = q.Symbol + ".json"
		} else {
			filename = "quote.json"
		}
	}
	csv := q.Highstock()
	return os.WriteFile(filename, []byte(csv), 0644)
}

// csvRows splits a csv payload into data rows of exactly want fields.
// The header row is dropped, as are blank lines: the Write* methods emit a
// trailing newline, which previously sized the Quote one bar too large and
// left a phantom all-zero bar at the end. Parsing stops at the first row with
// an unexpected field count, matching the original behavior.
func csvRows(csv string, want int) [][]string {
	lines := strings.Split(strings.ReplaceAll(csv, "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return nil
	}
	rows := make([][]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != want {
			break
		}
		rows = append(rows, fields)
	}
	return rows
}

// NewQuoteFromCSV - parse csv quote string into Quote structure
func NewQuoteFromCSV(symbol, csv string) (Quote, error) {

	rows := csvRows(csv, 6)
	q := NewQuote(symbol, len(rows))

	for bar, line := range rows {
		q.Date[bar], _ = time.Parse("2006-01-02 15:04", line[0])
		q.Open[bar], _ = strconv.ParseFloat(line[1], 64)
		q.High[bar], _ = strconv.ParseFloat(line[2], 64)
		q.Low[bar], _ = strconv.ParseFloat(line[3], 64)
		q.Close[bar], _ = strconv.ParseFloat(line[4], 64)
		q.Volume[bar], _ = strconv.ParseFloat(line[5], 64)
	}
	return q, nil
}

// NewQuoteFromCSVDateFormat - parse csv quote string into Quote structure
// with specified DateTime format
func NewQuoteFromCSVDateFormat(symbol, csv string, format string) (Quote, error) {

	if len(strings.TrimSpace(format)) == 0 {
		format = "2006-01-02 15:04"
	}

	rows := csvRows(csv, 6)
	// NewQuote("", ...) here dropped the caller's symbol on the floor; the
	// sibling NewQuoteFromCSV always set it.
	q := NewQuote(symbol, len(rows))

	for bar, line := range rows {
		q.Date[bar], _ = time.Parse(format, line[0])
		q.Open[bar], _ = strconv.ParseFloat(line[1], 64)
		q.High[bar], _ = strconv.ParseFloat(line[2], 64)
		q.Low[bar], _ = strconv.ParseFloat(line[3], 64)
		q.Close[bar], _ = strconv.ParseFloat(line[4], 64)
		q.Volume[bar], _ = strconv.ParseFloat(line[5], 64)
	}
	return q, nil
}

// NewQuoteFromCSVFile - parse csv quote file into Quote structure
func NewQuoteFromCSVFile(symbol, filename string) (Quote, error) {
	csv, err := os.ReadFile(filename)
	if err != nil {
		return NewQuote("", 0), err
	}
	return NewQuoteFromCSV(symbol, string(csv))
}

// NewQuoteFromCSVFileDateFormat - parse csv quote file into Quote structure
// with specified DateTime format
func NewQuoteFromCSVFileDateFormat(symbol, filename string, format string) (Quote, error) {
	csv, err := os.ReadFile(filename)
	if err != nil {
		return NewQuote("", 0), err
	}
	return NewQuoteFromCSVDateFormat(symbol, string(csv), format)
}

// JSON - convert Quote struct to json string
func (q Quote) JSON(indent bool) string {
	var j []byte
	if indent {
		j, _ = json.MarshalIndent(q, "", "  ")
	} else {
		j, _ = json.Marshal(q)
	}
	return string(j)
}

// WriteJSON - write Quote struct to json file
// Deprecated: use Quote.WriteFile(filename, FormatJSON).
func (q Quote) WriteJSON(filename string, indent bool) error {
	if filename == "" {
		filename = q.Symbol + ".json"
	}
	json := q.JSON(indent)
	return os.WriteFile(filename, []byte(json), 0644)

}

// NewQuoteFromJSON - parse json quote string into Quote structure
func NewQuoteFromJSON(jsn string) (Quote, error) {
	q := Quote{}
	err := json.Unmarshal([]byte(jsn), &q)
	if err != nil {
		return q, err
	}
	return q, nil
}

// NewQuoteFromJSONFile - parse json quote string into Quote structure
func NewQuoteFromJSONFile(filename string) (Quote, error) {
	jsn, err := os.ReadFile(filename)
	if err != nil {
		return NewQuote("", 0), err
	}
	return NewQuoteFromJSON(string(jsn))
}

// CSV - convert Quotes structure to csv string
func (q Quotes) CSV() string {

	var buffer bytes.Buffer

	buffer.WriteString("symbol,datetime,open,high,low,close,volume\n")

	for sym := range q {
		quote := q[sym]
		precision := getPrecision(quote.Symbol)
		for bar := range quote.Close {
			str := fmt.Sprintf("%s,%s,%.*f,%.*f,%.*f,%.*f,%.*f\n",
				quote.Symbol, quote.Date[bar].Format("2006-01-02 15:04"), precision, quote.Open[bar], precision, quote.High[bar], precision, quote.Low[bar], precision, quote.Close[bar], precision, quote.Volume[bar])
			buffer.WriteString(str)
		}
	}

	return buffer.String()
}

// Highstock - convert Quotes structure to Highstock json format
func (q Quotes) Highstock() string {

	var buffer bytes.Buffer

	buffer.WriteString("{")

	for sym := range q {
		quote := q[sym]
		precision := getPrecision(quote.Symbol)
		// The opening `"sym":[` must be written unconditionally. Emitting it
		// inside the bar loop meant a symbol with zero bars produced a closing
		// bracket with no opener, making the whole document invalid JSON.
		buffer.WriteString(fmt.Sprintf("\"%s\":[\n", quote.Symbol))
		for bar := range quote.Close {
			comma := ","
			if bar == len(quote.Close)-1 {
				comma = ""
			}
			str := fmt.Sprintf("[%d,%.*f,%.*f,%.*f,%.*f,%.*f]%s\n",
				quote.Date[bar].UnixNano()/1000000, precision, quote.Open[bar], precision, quote.High[bar], precision, quote.Low[bar], precision, quote.Close[bar], precision, quote.Volume[bar], comma)
			buffer.WriteString(str)
		}
		if sym < len(q)-1 {
			buffer.WriteString("],\n")
		} else {
			buffer.WriteString("]\n")
		}
	}

	buffer.WriteString("}")

	return buffer.String()
}

// Amibroker - convert Quotes structure to csv string
func (q Quotes) Amibroker() string {

	var buffer bytes.Buffer

	buffer.WriteString("symbol,date,time,open,high,low,close,volume\n")

	for sym := range q {
		quote := q[sym]
		precision := getPrecision(quote.Symbol)
		for bar := range quote.Close {
			str := fmt.Sprintf("%s,%s,%s,%.*f,%.*f,%.*f,%.*f,%.*f\n",
				quote.Symbol, quote.Date[bar].Format("2006-01-02"), quote.Date[bar].Format("15:04"), precision, quote.Open[bar], precision, quote.High[bar], precision, quote.Low[bar], precision, quote.Close[bar], precision, quote.Volume[bar])
			buffer.WriteString(str)
		}
	}

	return buffer.String()
}

// WriteCSV - write Quotes structure to file
// Deprecated: use Quotes.WriteFile(filename, FormatCSV).
func (q Quotes) WriteCSV(filename string) error {
	if filename == "" {
		filename = "quotes.csv"
	}
	csv := q.CSV()
	ba := []byte(csv)
	return os.WriteFile(filename, ba, 0644)
}

// WriteAmibroker - write Quotes structure to file
// Deprecated: use Quotes.WriteFile(filename, FormatAmibroker).
func (q Quotes) WriteAmibroker(filename string) error {
	if filename == "" {
		filename = "quotes.csv"
	}
	csv := q.Amibroker()
	ba := []byte(csv)
	return os.WriteFile(filename, ba, 0644)
}

// NewQuotesFromCSV - parse csv quote string into Quotes array
func NewQuotesFromCSV(csv string) (Quotes, error) {

	quotes := Quotes{}
	rows := csvRows(csv, 7)

	// Count bars per symbol, and record first-appearance order separately.
	// Ranging over the map directly would consume rows in randomized order,
	// assigning each symbol another symbol's bars.
	var index = make(map[string]int)
	var order []string
	for _, line := range rows {
		sym := line[0]
		if _, seen := index[sym]; !seen {
			order = append(order, sym)
		}
		index[sym]++
	}

	row := 0
	for _, sym := range order {
		bars := index[sym]
		q := NewQuote(sym, bars)
		for bar := range bars {
			line := rows[row]
			q.Date[bar], _ = time.Parse("2006-01-02 15:04", line[1])
			q.Open[bar], _ = strconv.ParseFloat(line[2], 64)
			q.High[bar], _ = strconv.ParseFloat(line[3], 64)
			q.Low[bar], _ = strconv.ParseFloat(line[4], 64)
			q.Close[bar], _ = strconv.ParseFloat(line[5], 64)
			q.Volume[bar], _ = strconv.ParseFloat(line[6], 64)
			row++
		}
		quotes = append(quotes, q)
	}
	return quotes, nil
}

// NewQuotesFromCSVFile - parse csv quote file into Quotes array
func NewQuotesFromCSVFile(filename string) (Quotes, error) {
	csv, err := os.ReadFile(filename)
	if err != nil {
		return Quotes{}, err
	}
	return NewQuotesFromCSV(string(csv))
}

// JSON - convert Quotes struct to json string
func (q Quotes) JSON(indent bool) string {
	var j []byte
	if indent {
		j, _ = json.MarshalIndent(q, "", "  ")
	} else {
		j, _ = json.Marshal(q)
	}
	return string(j)
}

// WriteJSON - write Quote struct to json file
// Deprecated: use Quotes.WriteFile(filename, FormatJSON).
func (q Quotes) WriteJSON(filename string, indent bool) error {
	if filename == "" {
		filename = "quotes.json"
	}
	jsn := q.JSON(indent)
	return os.WriteFile(filename, []byte(jsn), 0644)
}

// WriteHighstock - write Quote struct to json file in Highstock format
// Deprecated: use Quotes.WriteFile(filename, FormatHighstock).
func (q Quotes) WriteHighstock(filename string) error {
	if filename == "" {
		filename = "quotes.json"
	}
	hc := q.Highstock()
	return os.WriteFile(filename, []byte(hc), 0644)
}

// NewQuotesFromJSON - parse json quote string into Quote structure
func NewQuotesFromJSON(jsn string) (Quotes, error) {
	quotes := Quotes{}
	err := json.Unmarshal([]byte(jsn), &quotes)
	if err != nil {
		return quotes, err
	}
	return quotes, nil
}

// NewQuotesFromJSONFile - parse json quote string into Quote structure
func NewQuotesFromJSONFile(filename string) (Quotes, error) {
	jsn, err := os.ReadFile(filename)
	if err != nil {
		return Quotes{}, err
	}
	return NewQuotesFromJSON(string(jsn))
}

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

// UpdateFileTiingo updates an existing CSV (single or multi-symbol) in place using Tiingo daily data.
// It preserves original symbol order, rewrites a small overlap window (backfillDays),
// and optionally fully re-downloads a symbol's history if a corporate action is detected in the overlap.
func UpdateFileTiingo(path string, token string, backfillDays int, fullRedownload bool, concurrency int, end time.Time) error {
	if backfillDays < 0 {
		backfillDays = 0
	}
	if concurrency < 1 {
		concurrency = 1
	}
	// Determine file type by header
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanLines)
	if !scanner.Scan() {
		return fmt.Errorf("empty file: %s", path)
	}
	header := strings.TrimSpace(scanner.Text())
	// Normalize potential CRLF
	header = strings.TrimSuffix(header, "\r")

	if strings.HasPrefix(strings.ToLower(header), "symbol,datetime,") {
		return updateMultiTiingo(path, header, token, backfillDays, fullRedownload, concurrency, end)
	} else if strings.HasPrefix(strings.ToLower(header), "datetime,") {
		// infer symbol from filename
		base := filepath.Base(path)
		sym := strings.TrimSuffix(base, filepath.Ext(base))
		if sym == "" {
			return fmt.Errorf("cannot infer symbol from filename: %s", path)
		}
		return updateSingleTiingo(path, header, sym, token, backfillDays, fullRedownload, end)
	}
	return fmt.Errorf("unrecognized CSV header: %s", header)
}

// --- Internal helpers for update mode ---

// SymbolNotFoundError indicates that a symbol was not found in the data source.
// This error type allows callers to distinguish "not found" from other errors.
type SymbolNotFoundError struct {
	Symbol string
}

func (e *SymbolNotFoundError) Error() string {
	return fmt.Sprintf("symbol '%s' not found", e.Symbol)
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

// fetchTiingoDailyRaw returns the raw daily records from Tiingo between [from, to].
// Retained as the seam that update mode fetches through; it now shares the
// URL building and transport with the quote path rather than duplicating both.
func fetchTiingoDailyRaw(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
	return DefaultClient.fetchTiingoDaily(context.Background(), symbol, from, to, Daily, token)
}

// tiingoFetch is a package-level indirection for testing.
var tiingoFetch = fetchTiingoDailyRaw

// parseCSVDate parses the datetime string used in CSV files.
func parseCSVDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04", s)
}

// tquotesToLines converts raw Tiingo quotes to CSV lines matching our formats.
func tquotesToLines(symbol string, tq []tquoteRaw, multi bool) []string {
	precision := getPrecision(symbol)
	lines := make([]string, 0, len(tq))
	for _, r := range tq {
		// Parse YYYY-MM-DD, emit with HH:MM (00:00)
		dt, _ := time.Parse("2006-01-02", r.Date[0:10])
		if multi {
			lines = append(lines, fmt.Sprintf("%s,%s,%.*f,%.*f,%.*f,%.*f,%.*f",
				symbol,
				dt.Format("2006-01-02 15:04"),
				precision, r.AdjOpen,
				precision, r.AdjHigh,
				precision, r.AdjLow,
				precision, r.AdjClose,
				precision, r.Volume,
			))
		} else {
			lines = append(lines, fmt.Sprintf("%s,%.*f,%.*f,%.*f,%.*f,%.*f",
				dt.Format("2006-01-02 15:04"),
				precision, r.AdjOpen,
				precision, r.AdjHigh,
				precision, r.AdjLow,
				precision, r.AdjClose,
				precision, r.Volume,
			))
		}
	}
	return lines
}

// detectCA returns true if any split/dividend is present in the raw quotes slice.
func detectCA(tq []tquoteRaw) bool {
	for _, r := range tq {
		if r.SplitFactor != 1.0 || r.DivCash != 0.0 {
			return true
		}
	}
	return false
}

// updateSingleTiingo updates a single-symbol CSV file in place.
func updateSingleTiingo(path, header, symbol, token string, backfillDays int, fullRedownload bool, end time.Time) error {
	// First pass: find earliest and last dates
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	sc := bufio.NewScanner(in)
	sc.Split(bufio.ScanLines)
	if !sc.Scan() {
		return fmt.Errorf("empty file: %s", path)
	}
	// header already captured, continue scanning data
	var have bool
	var earliest, last time.Time
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 6 {
			continue
		}
		dt, err := parseCSVDate(strings.TrimSpace(cols[0]))
		if err != nil {
			continue
		}
		if !have {
			earliest = dt
			last = dt
			have = true
		} else {
			if dt.Before(earliest) {
				earliest = dt
			}
			if dt.After(last) {
				last = dt
			}
		}
	}
	if !have {
		return fmt.Errorf("no data rows in %s", path)
	}

	cutoff := last.AddDate(0, 0, -backfillDays)
	if cutoff.Before(earliest) {
		cutoff = earliest
	}

	if end.IsZero() {
		end = time.Now()
	}

	// Prefetch overlap/new range and check CA
	raw, err := tiingoFetch(symbol, cutoff, end, token)
	if err != nil {
		var notFoundErr *SymbolNotFoundError
		if errors.As(err, &notFoundErr) {
			Log.Printf("symbol '%s' not found, skipping update", symbol)
			return nil
		}
		return err
	}
	if fullRedownload && detectCA(raw) {
		Log.Printf("corporate action detected for %s; redownloading full history from %s", symbol, earliest.Format("2006-01-02"))
		cutoff = earliest
		raw, err = tiingoFetch(symbol, cutoff, end, token)
		if err != nil {
			var notFoundErr *SymbolNotFoundError
			if errors.As(err, &notFoundErr) {
				Log.Printf("symbol '%s' not found, skipping update", symbol)
				return nil
			}
			return err
		}
	}
	updateLines := tquotesToLines(symbol, raw, false)

	// Second pass: rewrite file with cutoff and append updates
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// do not defer close; close explicitly before rename

	// Write original header exactly
	if _, err := out.WriteString(header + "\n"); err != nil {
		return err
	}

	// Reopen for scanning
	in2, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in2.Close()
	sc2 := bufio.NewScanner(in2)
	sc2.Split(bufio.ScanLines)
	// skip header
	_ = sc2.Scan()
	for sc2.Scan() {
		line := strings.TrimSpace(sc2.Text())
		if line == "" {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 6 {
			continue
		}
		dt, err := parseCSVDate(strings.TrimSpace(cols[0]))
		if err != nil {
			continue
		}
		if dt.Before(cutoff) {
			if _, err := out.WriteString(line + "\n"); err != nil {
				return err
			}
		}
	}

	// Append update lines
	for _, l := range updateLines {
		if _, err := out.WriteString(l + "\n"); err != nil {
			return err
		}
	}

	if err := out.Close(); err != nil {
		return err
	}
	// Atomic replace
	return os.Rename(tmp, path)
}

// updateMultiTiingo updates a multi-symbol CSV file (symbol as first column) in place.
func updateMultiTiingo(path, header, token string, backfillDays int, fullRedownload bool, concurrency int, end time.Time) error {
	if end.IsZero() {
		end = time.Now()
	}

	// First pass: determine order, earliest and last per symbol
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	sc := bufio.NewScanner(in)
	sc.Split(bufio.ScanLines)
	if !sc.Scan() {
		return fmt.Errorf("empty file: %s", path)
	}

	order := []string{}
	earliest := map[string]time.Time{}
	last := map[string]time.Time{}
	seen := map[string]bool{}
	nonContiguous := false
	var prevSym string
	firstData := true

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 7 {
			continue
		}
		sym := strings.TrimSpace(cols[0])
		dt, err := parseCSVDate(strings.TrimSpace(cols[1]))
		if err != nil {
			continue
		}
		if !seen[sym] {
			order = append(order, sym)
			earliest[sym] = dt
			last[sym] = dt
			seen[sym] = true
		} else {
			if dt.Before(earliest[sym]) {
				earliest[sym] = dt
			}
			if dt.After(last[sym]) {
				last[sym] = dt
			}
			if !firstData && sym != prevSym {
				// if we have seen sym before and it reappears later, it's non-contiguous
				nonContiguous = true
			}
		}
		prevSym = sym
		firstData = false
	}

	if nonContiguous {
		Log.Println("warning: input not grouped by symbol; proceeding but order may be suboptimal")
	}

	// Prefetch updates per symbol (concurrently)
	cutoffMap := map[string]time.Time{}
	linesMap := map[string][]string{}
	var mu sync.Mutex
	type fetchErr struct {
		sym string
		err error
	}
	errCh := make(chan fetchErr, len(order))
	var wg sync.WaitGroup

	// optional global rate limiter
	var limiter *time.Ticker
	if Delay > 0 {
		limiter = time.NewTicker(Delay * time.Millisecond)
		defer limiter.Stop()
	}

	jobs := make(chan string, len(order))
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				c := last[sym].AddDate(0, 0, -backfillDays)
				if c.Before(earliest[sym]) {
					c = earliest[sym]
				}
				if limiter != nil {
					<-limiter.C
				}
				raw, err := tiingoFetch(sym, c, end, token)
				if err != nil {
					errCh <- fetchErr{sym: sym, err: err}
					continue
				}
				if fullRedownload && detectCA(raw) {
					Log.Printf("corporate action detected for %s; redownloading full history from %s", sym, earliest[sym].Format("2006-01-02"))
					c = earliest[sym]
					if limiter != nil {
						<-limiter.C
					}
					raw, err = tiingoFetch(sym, c, end, token)
					if err != nil {
						errCh <- fetchErr{sym: sym, err: err}
						continue
					}
				}
				lines := tquotesToLines(sym, raw, true)
				mu.Lock()
				cutoffMap[sym] = c
				linesMap[sym] = lines
				mu.Unlock()
			}
		}()
	}
	for _, sym := range order {
		jobs <- sym
	}
	close(jobs)
	wg.Wait()
	close(errCh)
	var errs []string
	for fe := range errCh {
		var notFoundErr *SymbolNotFoundError
		if errors.As(fe.err, &notFoundErr) {
			// Log "not found" as info, don't treat as error
			Log.Printf("symbol '%s' not found, skipping", fe.sym)
		} else {
			// Real errors get collected
			errs = append(errs, fmt.Sprintf("%s: %v", fe.sym, fe.err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("update fetch errors (%d): %s", len(errs), strings.Join(errs, "; "))
	}

	// Second pass: rewrite with preserved order and per-symbol cutoffs
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// do not defer close; close explicitly before rename
	if _, err := out.WriteString(header + "\n"); err != nil {
		return err
	}

	in2, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in2.Close()
	sc2 := bufio.NewScanner(in2)
	sc2.Split(bufio.ScanLines)
	// skip header
	_ = sc2.Scan()

	writtenUpdate := map[string]bool{}
	prevSym = ""
	firstData = true
	for sc2.Scan() {
		line := strings.TrimSpace(sc2.Text())
		if line == "" {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 7 {
			continue
		}
		sym := strings.TrimSpace(cols[0])
		dt, err := parseCSVDate(strings.TrimSpace(cols[1]))
		if err != nil {
			continue
		}
		if firstData {
			prevSym = sym
			firstData = false
		}
		if sym != prevSym {
			if !writtenUpdate[prevSym] {
				for _, l := range linesMap[prevSym] {
					if _, err := out.WriteString(l + "\n"); err != nil {
						return err
					}
				}
				writtenUpdate[prevSym] = true
			}
			prevSym = sym
		}
		if dt.Before(cutoffMap[sym]) {
			if _, err := out.WriteString(line + "\n"); err != nil {
				return err
			}
		}
	}
	// Flush last symbol's updates
	if prevSym != "" && !writtenUpdate[prevSym] {
		for _, l := range linesMap[prevSym] {
			if _, err := out.WriteString(l + "\n"); err != nil {
				return err
			}
		}
		writtenUpdate[prevSym] = true
	}

	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
			q.Date[bar] = time.Unix(int64(bars[row][0]), 0)
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

// FetchFunc fetches one symbol. FetchAll drives it across a symbol list.
type FetchFunc func(ctx context.Context, symbol string) (Quote, error)

// FetchAll fetches every symbol in order, pausing Client.Delay between
// requests and skipping symbols that fail. It replaces four copies of the
// same append/log/sleep loop, one per data source.
func (c *Client) FetchAll(ctx context.Context, symbols []string, fetch FetchFunc) (Quotes, error) {
	quotes := Quotes{}
	for i, symbol := range symbols {
		if i > 0 {
			if err := sleep(ctx, c.rateLimit()); err != nil {
				return quotes, err
			}
		}
		quote, err := fetch(ctx, symbol)
		if err != nil {
			c.logger().Println("error downloading " + symbol)
			continue
		}
		quotes = append(quotes, quote)
	}
	if err := ctx.Err(); err != nil {
		return quotes, err
	}
	return quotes, nil
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

// CoinbaseSyms fetches Coinbase prices for a list of symbols.
func (c *Client) CoinbaseSyms(ctx context.Context, symbols []string, from, to time.Time, period Period) (Quotes, error) {
	return c.FetchAll(ctx, symbols, func(ctx context.Context, symbol string) (Quote, error) {
		return c.Coinbase(ctx, symbol, from, to, period)
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

// Grab a file via anonymous FTP
func getAnonFTP(addr, port string, dir string, fname string) ([]byte, error) {

	var err error
	var contents []byte
	const timeout = 5 * time.Second

	nconn, err := net.DialTimeout("tcp", addr+":"+port, timeout)
	if err != nil {
		return contents, err
	}
	defer nconn.Close()

	conn := textproto.NewConn(nconn)
	_, _, _ = conn.ReadResponse(2)
	defer conn.Close()

	_ = conn.PrintfLine("USER anonymous")
	_, _, _ = conn.ReadResponse(0)

	_ = conn.PrintfLine("PASS anonymous")
	_, _, _ = conn.ReadResponse(230)

	_ = conn.PrintfLine("CWD %s", dir)
	_, _, _ = conn.ReadResponse(250)

	_ = conn.PrintfLine("PASV")
	_, message, _ := conn.ReadResponse(1)

	// PASV response format : 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2).
	start, end := strings.Index(message, "("), strings.Index(message, ")")
	s := strings.Split(message[start:end], ",")
	l1, _ := strconv.Atoi(s[len(s)-2])
	l2, _ := strconv.Atoi(s[len(s)-1])
	dport := l1*256 + l2

	_ = conn.PrintfLine("RETR %s", fname)
	_, _, _ = conn.ReadResponse(1)
	dconn, err := net.DialTimeout("tcp", addr+":"+strconv.Itoa(dport), timeout)
	if err != nil {
		return contents, err
	}
	defer dconn.Close()

	contents, err = io.ReadAll(dconn)
	if err != nil {
		return contents, err
	}

	_ = dconn.Close()
	_, _, _ = conn.ReadResponse(2)

	return contents, nil
}
