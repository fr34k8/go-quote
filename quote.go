/*
Package quote is free quote downloader library and cli

# Downloads historical price quotes from Tiingo and Coinbase

Copyright 2025 Mark Chenoweth
Licensed under terms of MIT license (see LICENSE)
*/
package quote

import (
    "bufio"
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log"
    "math/rand"
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
var Log *log.Logger

// Delay - time delay in milliseconds between quote requests (default=100)
// Be nice, don't get blocked
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

// NewQuoteFromCSV - parse csv quote string into Quote structure
func NewQuoteFromCSV(symbol, csv string) (Quote, error) {

	tmp := strings.Split(csv, "\n")
	numrows := len(tmp)
	q := NewQuote(symbol, numrows-1)

	for row, bar := 1, 0; row < numrows; row, bar = row+1, bar+1 {
		line := strings.Split(tmp[row], ",")
		if len(line) != 6 {
			break
		}
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

	tmp := strings.Split(csv, "\n")
	numrows := len(tmp)
	q := NewQuote("", numrows-1)

	if len(strings.TrimSpace(format)) == 0 {
		format = "2006-01-02 15:04"
	}

	for row, bar := 1, 0; row < numrows; row, bar = row+1, bar+1 {
		line := strings.Split(tmp[row], ",")
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

	for sym := 0; sym < len(q); sym++ {
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

	for sym := 0; sym < len(q); sym++ {
		quote := q[sym]
		precision := getPrecision(quote.Symbol)
		for bar := range quote.Close {
			comma := ","
			if bar == len(quote.Close)-1 {
				comma = ""
			}
			if bar == 0 {
				buffer.WriteString(fmt.Sprintf("\"%s\":[\n", quote.Symbol))
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

	for sym := 0; sym < len(q); sym++ {
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
func (q Quotes) WriteCSV(filename string) error {
	if filename == "" {
		filename = "quotes.csv"
	}
	csv := q.CSV()
	ba := []byte(csv)
	return os.WriteFile(filename, ba, 0644)
}

// WriteAmibroker - write Quotes structure to file
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
	tmp := strings.Split(csv, "\n")
	numrows := len(tmp)

	var index = make(map[string]int)
	for idx := 1; idx < numrows; idx++ {
		sym := strings.Split(tmp[idx], ",")[0]
		index[sym]++
	}

	row := 1
	for sym, len := range index {
		q := NewQuote(sym, len)
		for bar := 0; bar < len; bar++ {
			line := strings.Split(tmp[row], ",")
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
func (q Quotes) WriteJSON(filename string, indent bool) error {
	if filename == "" {
		filename = "quotes.json"
	}
	jsn := q.JSON(indent)
	return os.WriteFile(filename, []byte(jsn), 0644)
}

// WriteHighstock - write Quote struct to json file in Highstock format
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

// pickRandomUserAgent selects a random user agent from the list
func pickRandomUserAgent() string {
	var USER_AGENTS = []string{
		// Chrome
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",

		// Firefox
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.7; rv:135.0) Gecko/20100101 Firefox/135.0",
		"Mozilla/5.0 (X11; Linux i686; rv:135.0) Gecko/20100101 Firefox/135.0",

		// Safari
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.3 Safari/605.1.15",

		// Edge
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 Edg/131.0.2903.86",
	}

	rand.NewSource(time.Now().UnixNano())
	return USER_AGENTS[rand.Intn(len(USER_AGENTS))]
}

func tiingoDaily(symbol string, from, to time.Time, period Period, token string) (Quote, error) {

	type tquote struct {
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

	var tiingo []tquote

	urlStr := fmt.Sprintf(
		"https://api.tiingo.com/tiingo/daily/%s/prices?startDate=%s&endDate=%s",
		strings.TrimSpace(strings.Replace(symbol, "/", "-", -1)),
		url.QueryEscape(from.Format("2006-1-2")),
		url.QueryEscape(to.Format("2006-1-2")))

	if period == Weekly {
		urlStr += "&resampleFreq=weekly"
	} else if period == Monthly {
		urlStr += "&resampleFreq=monthly"
	}

	client := &http.Client{Timeout: ClientTimeout}
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
	resp, err := client.Do(req)

	if err != nil {
		Log.Printf("tiingo error: %v\n", err)
		return NewQuote("", 0), err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		contents, _ := io.ReadAll(resp.Body)
		err = json.Unmarshal(contents, &tiingo)
		if err != nil {
			Log.Printf("tiingo error: %v\n", err)
			return NewQuote("", 0), err
		}
	} else if resp.StatusCode == http.StatusNotFound {
		Log.Printf("symbol '%s' not found\n", symbol)
		return NewQuote("", 0), err
	}

	numrows := len(tiingo)
	quote := NewQuote(symbol, numrows)

	for bar := 0; bar < numrows; bar++ {
		quote.Date[bar], _ = time.Parse("2006-01-02", tiingo[bar].Date[0:10])
		quote.Open[bar] = tiingo[bar].AdjOpen
		quote.High[bar] = tiingo[bar].AdjHigh
		quote.Low[bar] = tiingo[bar].AdjLow
		quote.Close[bar] = tiingo[bar].AdjClose
		quote.Volume[bar] = float64(tiingo[bar].Volume)
	}

	return quote, nil
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
func fetchTiingoDailyRaw(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
    var tiingo []tquoteRaw

    urlStr := fmt.Sprintf(
        "https://api.tiingo.com/tiingo/daily/%s/prices?startDate=%s&endDate=%s",
        strings.TrimSpace(strings.Replace(symbol, "/", "-", -1)),
        url.QueryEscape(from.Format("2006-1-2")),
        url.QueryEscape(to.Format("2006-1-2")),
    )

    client := &http.Client{Timeout: ClientTimeout}
    req, _ := http.NewRequest("GET", urlStr, nil)
    req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        contents, _ := io.ReadAll(resp.Body)
        if err := json.Unmarshal(contents, &tiingo); err != nil {
            return nil, err
        }
        return tiingo, nil
    }
    if resp.StatusCode == http.StatusNotFound {
        return nil, &SymbolNotFoundError{Symbol: symbol}
    }
    return nil, fmt.Errorf("tiingo http status %d", resp.StatusCode)
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

    if err := out.Close(); err != nil { return err }
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
    type fetchErr struct{ sym string; err error }
    errCh := make(chan fetchErr, len(order))
    var wg sync.WaitGroup

    // optional global rate limiter
    var limiter *time.Ticker
    if Delay > 0 {
        limiter = time.NewTicker(Delay * time.Millisecond)
        defer limiter.Stop()
    }

    jobs := make(chan string, len(order))
    for i := 0; i < concurrency; i++ {
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

    if err := out.Close(); err != nil { return err }
    return os.Rename(tmp, path)
}

func tiingoCrypto(symbol string, from, to time.Time, period Period, token string) (Quote, error) {

	resampleFreq := "1day"
	switch period {
	case Min1:
		resampleFreq = "1min"
	case Min3:
		resampleFreq = "3min"
	case Min5:
		resampleFreq = "5min"
	case Min15:
		resampleFreq = "15min"
	case Min30:
		resampleFreq = "30min"
	case Min60:
		resampleFreq = "1hour"
	case Hour2:
		resampleFreq = "2hour"
	case Hour4:
		resampleFreq = "4hour"
	case Hour6:
		resampleFreq = "6hour"
	case Hour8:
		resampleFreq = "8hour"
	case Hour12:
		resampleFreq = "12hour"
	case Daily:
		resampleFreq = "1day"
	}

	type priceData struct {
		TradesDone     float64 `json:"tradesDone"`
		Close          float64 `json:"close"`
		VolumeNotional float64 `json:"volumeNotional"`
		Low            float64 `json:"low"`
		Open           float64 `json:"open"`
		Date           string  `json:"date"` // "2017-12-19T00:00:00Z"
		High           float64 `json:"high"`
		Volume         float64 `json:"volume"`
	}

	type cryptoData struct {
		Ticker        string      `json:"ticker"`
		BaseCurrency  string      `json:"baseCurrency"`
		QuoteCurrency string      `json:"quoteCurrency"`
		PriceData     []priceData `json:"priceData"`
	}

	var crypto []cryptoData

	url := fmt.Sprintf(
		"https://api.tiingo.com/tiingo/crypto/prices?tickers=%s&startDate=%s&endDate=%s&resampleFreq=%s",
		symbol,
		url.QueryEscape(from.Format("2006-1-2")),
		url.QueryEscape(to.Format("2006-1-2")),
		resampleFreq)

	client := &http.Client{Timeout: ClientTimeout}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
	resp, err := client.Do(req)

	if err != nil {
		Log.Printf("symbol '%s' not found\n", symbol)
		return NewQuote("", 0), err
	}
	defer resp.Body.Close()

	contents, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(contents, &crypto)
	if err != nil {
		Log.Printf("tiingo crypto symbol '%s' error: %v\n", symbol, err)
		return NewQuote("", 0), err
	}
	if len(crypto) < 1 {
		Log.Printf("tiingo crypto symbol '%s' No data returned", symbol)
		return NewQuote("", 0), err
	}

	numrows := len(crypto[0].PriceData)
	quote := NewQuote(symbol, numrows)

	for bar := 0; bar < numrows; bar++ {
		quote.Date[bar], _ = time.Parse(time.RFC3339, crypto[0].PriceData[bar].Date)
		quote.Open[bar] = crypto[0].PriceData[bar].Open
		quote.High[bar] = crypto[0].PriceData[bar].High
		quote.Low[bar] = crypto[0].PriceData[bar].Low
		quote.Close[bar] = crypto[0].PriceData[bar].Close
		quote.Volume[bar] = float64(crypto[0].PriceData[bar].Volume)
	}

	return quote, nil
}

// NewQuoteFromTiingo - Tiingo daily historical prices for a symbol
func NewQuoteFromTiingo(symbol, startDate, endDate string, period Period, token string) (Quote, error) {

	from := ParseDateString(startDate)
	to := ParseDateString(endDate)

	return tiingoDaily(symbol, from, to, period, token)
}

// NewQuoteFromTiingoCrypto - Tiingo crypto historical prices for a symbol
func NewQuoteFromTiingoCrypto(symbol, startDate, endDate string, period Period, token string) (Quote, error) {

	from := ParseDateString(startDate)
	to := ParseDateString(endDate)

	return tiingoCrypto(symbol, from, to, period, token)
}

// NewQuotesFromTiingoSyms - create a list of prices from symbols in string array
func NewQuotesFromTiingoSyms(symbols []string, startDate, endDate string, period Period, token string) (Quotes, error) {

	quotes := Quotes{}
	for _, symbol := range symbols {
		quote, err := NewQuoteFromTiingo(symbol, startDate, endDate, period, token)
		if err == nil {
			quotes = append(quotes, quote)
		} else {
			Log.Println("error downloading " + symbol)
		}
		time.Sleep(Delay * time.Millisecond)
	}
	return quotes, nil
}

// NewQuotesFromTiingoCryptoSyms - create a list of prices from symbols in string array
func NewQuotesFromTiingoCryptoSyms(symbols []string, startDate, endDate string, period Period, token string) (Quotes, error) {

	quotes := Quotes{}
	for _, symbol := range symbols {
		quote, err := NewQuoteFromTiingoCrypto(symbol, startDate, endDate, period, token)
		if err == nil {
			quotes = append(quotes, quote)
		} else {
			Log.Println("error downloading " + symbol)
		}
		time.Sleep(Delay * time.Millisecond)
	}
	return quotes, nil
}

// NewQuoteFromCoinbase - Coinbase Pro historical prices for a symbol
func NewQuoteFromCoinbase(symbol, startDate, endDate string, period Period) (Quote, error) {

	start := ParseDateString(startDate) //.In(time.Now().Location())
	end := ParseDateString(endDate)     //.In(time.Now().Location())

	var granularity int // seconds

	switch period {
	case Min1:
		granularity = 60
	case Min5:
		granularity = 5 * 60
	case Min15:
		granularity = 15 * 60
	case Min30:
		granularity = 30 * 60
	case Min60:
		granularity = 60 * 60
	case Daily:
		granularity = 24 * 60 * 60
	case Weekly:
		granularity = 7 * 24 * 60 * 60
	default:
		granularity = 24 * 60 * 60
	}

	var quote Quote
	quote.Symbol = symbol

	maxBars := 200
	var step = time.Second * time.Duration(granularity)

	startBar := start
	endBar := startBar.Add(time.Duration(maxBars) * step)

	if endBar.After(end) {
		endBar = end
	}

	//Log.Printf("startBar=%v, endBar=%v\n", startBar, endBar)

	for startBar.Before(end) {

		url := fmt.Sprintf(
			"https://api.exchange.coinbase.com/products/%s/candles?start=%s&end=%s&granularity=%d",
			symbol,
			url.QueryEscape(startBar.Format(time.RFC3339)),
			url.QueryEscape(endBar.Format(time.RFC3339)),
			granularity)

		client := &http.Client{Timeout: ClientTimeout}
		req, _ := http.NewRequest("GET", url, nil)
		resp, err := client.Do(req)

		if err != nil {
			Log.Printf("coinbase error: %v\n", err)
			return NewQuote("", 0), err
		}
		defer resp.Body.Close()

		contents, _ := io.ReadAll(resp.Body)

		type cb [6]float64
		var bars []cb
		err = json.Unmarshal(contents, &bars)
		if err != nil {
			Log.Printf("coinbase error: %v\n", err)
		}

		numrows := len(bars)
		q := NewQuote(symbol, numrows)

		//Log.Printf("numrows=%d, bars=%v\n", numrows, bars)

		for row := 0; row < numrows; row++ {
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

		time.Sleep(time.Second)
		startBar = endBar.Add(step)
		endBar = startBar.Add(time.Duration(maxBars) * step)

	}

	return quote, nil
}

// NewQuotesFromCoinbase - create a list of prices from symbols in file
func NewQuotesFromCoinbase(filename, startDate, endDate string, period Period) (Quotes, error) {

	quotes := Quotes{}
	inFile, err := os.Open(filename)
	if err != nil {
		return quotes, err
	}
	defer inFile.Close()
	scanner := bufio.NewScanner(inFile)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		sym := scanner.Text()
		quote, err := NewQuoteFromCoinbase(sym, startDate, endDate, period)
		if err == nil {
			quotes = append(quotes, quote)
		} else {
			Log.Println("error downloading " + sym)
		}
		time.Sleep(Delay * time.Millisecond)
	}
	return quotes, nil
}

// NewQuotesFromCoinbaseSyms - create a list of prices from symbols in string array
func NewQuotesFromCoinbaseSyms(symbols []string, startDate, endDate string, period Period) (Quotes, error) {

	quotes := Quotes{}
	for _, symbol := range symbols {
		quote, err := NewQuoteFromCoinbase(symbol, startDate, endDate, period)
		if err == nil {
			quotes = append(quotes, quote)
		} else {
			Log.Println("error downloading " + symbol)
		}
		time.Sleep(Delay * time.Millisecond)
	}
	return quotes, nil
}

// NewEtfList - download a list of etf symbols to an array of strings
func NewEtfList() ([]string, error) {

	var symbols []string

	buf, err := getAnonFTP("ftp.nasdaqtrader.com", "21", "symboldirectory", "otherlisted.txt")
	if err != nil {
		Log.Println(err)
		return symbols, err
	}

	for _, line := range strings.Split(string(buf), "\n") {
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
	if strings.HasPrefix(market, "tiingo") {
		if os.Getenv("TIINGO_API_TOKEN") == "" {
			fmt.Println("ERROR: Requires TIINGO_API_TOKEN to be set")
			return false
		}
	}
	for _, v := range ValidMarkets {
		if v == market {
			return true
		}
	}
	return false
}

// NewMarketList - download a list of market symbols to an array of strings
func NewMarketList(market string) ([]string, error) {

	var symbols []string
	if !ValidMarket(market) {
		return symbols, fmt.Errorf("invalid market")
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
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("User-Agent", "markcheno/go-quote")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return symbols, err
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	newStr := buf.String()

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
		log.Fatalf("Error parsing JSON: %v", err)
	}

	var symbols []string
	for _, row := range apiResponse.Data.Rows {
		symbols = append(symbols, strings.ToLower(row.Symbol))
		//fmt.Printf("Symbol: %s\n", row.Symbol)
	}

	sort.Strings(symbols)

	return symbols, err
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
		log.Fatalf("Error parsing JSON: %v", err)
	}

	var symbols []string
	for _, row := range apiResponse.Data.Data.Rows {
		symbols = append(symbols, strings.ToLower(row.Symbol))
		//fmt.Printf("Symbol: %s\n", row.Symbol)
	}

	sort.Strings(symbols)

	return symbols, err
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
	if err == nil {
		defer dconn.Close()
	}

	contents, err = io.ReadAll(dconn)
	if err != nil {
		return contents, err
	}

	_ = dconn.Close()
	_, _, _ = conn.ReadResponse(2)

	return contents, nil
}
