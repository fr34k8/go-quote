package quote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// assert fails the test if the condition is false.
func assert(t *testing.T, condition bool, msg string, v ...any) {
	if !condition {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("%s:%d: "+msg+"\n", append([]any{filepath.Base(file), line}, v...)...)
		t.FailNow()
	}
}

// ok fails the test if an err is not nil.
func ok(t *testing.T, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("%s:%d: unexpected error: %s\n", filepath.Base(file), line, err.Error())
		t.FailNow()
	}
}

// equals fails the test if exp is not equal to act.
func equals(t *testing.T, exp, act any) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("%s:%d:\n\texp: %#v\n\tgot: %#v\n", filepath.Base(file), line, exp, act)
		t.FailNow()
	}
}

// TODO - everything

func TestNewQuoteFromCSV(t *testing.T) {
	symbol := "aapl"
	csv := `datetime,open,high,low,close,volume
2014-07-14 00:00,95.86,96.89,95.65,88.40,42810000.00
2014-07-15 00:00,96.80,96.85,95.03,87.36,45477900.00
2014-07-16 00:00,96.97,97.10,94.74,86.87,53396300.00
2014-07-17 00:00,95.03,95.28,92.57,85.32,57298000.00
2014-07-18 00:00,93.62,94.74,93.02,86.55,49988000.00
2014-07-21 00:00,94.99,95.00,93.72,86.10,39079000.00
2014-07-22 00:00,94.68,94.89,94.12,86.81,55197000.00
2014-07-23 00:00,95.42,97.88,95.17,89.08,92918000.00
2014-07-24 00:00,97.04,97.32,96.42,88.93,45729000.00
2014-07-25 00:00,96.85,97.84,96.64,89.52,43469000.00
2014-07-28 00:00,97.82,99.24,97.55,90.75,55318000.00
2014-07-29 00:00,99.33,99.44,98.25,90.17,43143000.00
2014-07-30 00:00,98.44,98.70,97.67,89.96,33010000.00
2014-07-31 00:00,97.16,97.45,95.33,87.62,56843000.00`
	q, _ := NewQuoteFromCSV(symbol, csv)
	//fmt.Println(q)
	if len(q.Close) != 14 {
		t.Error("Invalid length")
	}
	if q.Close[len(q.Close)-1] != 87.62 {
		t.Error("Invalid last value")
	}
}

// --- Update mode tests and helpers ---

// helper to write a temp file with contents and return path and cleanup func
func writeTempFile(t *testing.T, name, contents string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path, func() {}
}

func TestUpdateSingle_NoCA(t *testing.T) {
	// Prepare initial single-symbol CSV with 5 days: Jan 1..5
	initial := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,1,1,1,10,100",
		"2025-01-02 00:00,1,1,1,11,100",
		"2025-01-03 00:00,1,1,1,12,100",
		"2025-01-04 00:00,1,1,1,13,100",
		"2025-01-05 00:00,1,1,1,14,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "spy.csv", initial)

	// Stub Tiingo fetch to return overlap (from cutoff=Jan 03) to Jan 07, with different close values
	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		tq := []tquoteRaw{}
		// from should be 2025-01-03
		for d := 3; d <= 7; d++ {
			date := time.Date(2025, 1, d, 0, 0, 0, 0, time.UTC)
			tq = append(tq, tquoteRaw{
				Date:        date.Format("2006-01-02"),
				AdjOpen:     2,
				AdjHigh:     3,
				AdjLow:      1,
				AdjClose:    float64(40 + d), // 43..47
				Volume:      200,
				SplitFactor: 1.0,
				DivCash:     0.0,
			})
		}
		return tq, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	end := time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC)
	if err := UpdateFileTiingo(path, "token", 2, false, 1, end); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	out, _ := os.ReadFile(path)
	got := string(out)
	// Expect preserved rows for Jan 1..2, then updates 3..7 with adjusted values
	want := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,1,1,1,10,100",
		"2025-01-02 00:00,1,1,1,11,100",
		"2025-01-03 00:00,2.00,3.00,1.00,43.00,200.00",
		"2025-01-04 00:00,2.00,3.00,1.00,44.00,200.00",
		"2025-01-05 00:00,2.00,3.00,1.00,45.00,200.00",
		"2025-01-06 00:00,2.00,3.00,1.00,46.00,200.00",
		"2025-01-07 00:00,2.00,3.00,1.00,47.00,200.00",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("single update mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

func TestUpdateMulti_Concurrency_OrderPreserved(t *testing.T) {
	// Initial multi CSV with aaa and bbb, 3 days each
	initial := strings.Join([]string{
		"symbol,datetime,open,high,low,close,volume",
		"aaa,2025-01-01 00:00,1,1,1,10,100",
		"aaa,2025-01-02 00:00,1,1,1,11,100",
		"aaa,2025-01-03 00:00,1,1,1,12,100",
		"bbb,2025-01-01 00:00,1,1,1,20,100",
		"bbb,2025-01-02 00:00,1,1,1,21,100",
		"bbb,2025-01-03 00:00,1,1,1,22,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "multi.csv", initial)

	// Stub per-symbol responses (overlap from Jan 02)
	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		tq := []tquoteRaw{}
		switch symbol {
		case "aaa":
			for d := 2; d <= 6; d++ { // 2..6
				date := time.Date(2025, 1, d, 0, 0, 0, 0, time.UTC)
				tq = append(tq, tquoteRaw{Date: date.Format("2006-01-02"), AdjOpen: 2, AdjHigh: 3, AdjLow: 1, AdjClose: float64(100 + d), Volume: 200, SplitFactor: 1.0})
			}
		case "bbb":
			for d := 2; d <= 4; d++ { // 2..4
				date := time.Date(2025, 1, d, 0, 0, 0, 0, time.UTC)
				tq = append(tq, tquoteRaw{Date: date.Format("2006-01-02"), AdjOpen: 5, AdjHigh: 6, AdjLow: 4, AdjClose: float64(200 + d), Volume: 300, SplitFactor: 1.0})
			}
		}
		return tq, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	end := time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC)
	if err := UpdateFileTiingo(path, "token", 1, false, 3, end); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got := string(must(os.ReadFile(path)))
	want := strings.Join([]string{
		"symbol,datetime,open,high,low,close,volume",
		// aaa preserved day 1
		"aaa,2025-01-01 00:00,1,1,1,10,100",
		// aaa updates 2..6
		"aaa,2025-01-02 00:00,2.00,3.00,1.00,102.00,200.00",
		"aaa,2025-01-03 00:00,2.00,3.00,1.00,103.00,200.00",
		"aaa,2025-01-04 00:00,2.00,3.00,1.00,104.00,200.00",
		"aaa,2025-01-05 00:00,2.00,3.00,1.00,105.00,200.00",
		"aaa,2025-01-06 00:00,2.00,3.00,1.00,106.00,200.00",
		// bbb preserved day 1
		"bbb,2025-01-01 00:00,1,1,1,20,100",
		// bbb updates 2..4
		"bbb,2025-01-02 00:00,5.00,6.00,4.00,202.00,300.00",
		"bbb,2025-01-03 00:00,5.00,6.00,4.00,203.00,300.00",
		"bbb,2025-01-04 00:00,5.00,6.00,4.00,204.00,300.00",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("multi update mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

func TestUpdateSingle_FullRedownloadOnCA(t *testing.T) {
	initial := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,1,1,1,10,100",
		"2025-01-02 00:00,1,1,1,11,100",
		"2025-01-03 00:00,1,1,1,12,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "abc.csv", initial)

	prev := tiingoFetch
	calls := 0
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		calls++
		// first call simulates CA in overlap; second call returns full history
		if calls == 1 {
			return []tquoteRaw{{Date: "2025-01-02", AdjOpen: 2, AdjHigh: 3, AdjLow: 1, AdjClose: 22, Volume: 200, SplitFactor: 2.0}}, nil
		}
		return []tquoteRaw{
			{Date: "2025-01-01", AdjOpen: 2, AdjHigh: 3, AdjLow: 1, AdjClose: 21, Volume: 200},
			{Date: "2025-01-02", AdjOpen: 2, AdjHigh: 3, AdjLow: 1, AdjClose: 22, Volume: 200},
			{Date: "2025-01-03", AdjOpen: 2, AdjHigh: 3, AdjLow: 1, AdjClose: 23, Volume: 200},
		}, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	end := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)
	if err := UpdateFileTiingo(path, "token", 1, true, 1, end); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got := string(must(os.ReadFile(path)))
	want := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,2.00,3.00,1.00,21.00,200.00",
		"2025-01-02 00:00,2.00,3.00,1.00,22.00,200.00",
		"2025-01-03 00:00,2.00,3.00,1.00,23.00,200.00",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("full re-download on CA mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

// small helper to panic on error in tests when reading
func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func TestNewQuotesFromCSV(t *testing.T) {
	csv := `symbol,datetime,open,high,low,close,volume
spy,2018-07-12 00:00,278.28,279.43,277.60,273.95,60124700.00
spy,2018-07-13 00:00,279.17,279.93,278.66,274.17,48216000.00
spy,2018-07-16 00:00,279.64,279.80,278.84,273.92,48201000.00
spy,2018-07-17 00:00,278.47,280.91,278.41,275.03,52315500.00
spy,2018-07-18 00:00,280.56,281.18,280.06,275.61,44593500.00
spy,2018-07-19 00:00,280.31,280.74,279.46,274.57,61412100.00
spy,2018-07-20 00:00,279.77,280.48,279.50,274.26,82337700.00
aapl,2018-07-12 00:00,189.53,191.41,189.31,188.17,18041100.00
aapl,2018-07-13 00:00,191.08,191.84,190.90,188.46,12513900.00
aapl,2018-07-16 00:00,191.52,192.65,190.42,188.05,15043100.00
aapl,2018-07-17 00:00,189.75,191.87,189.20,188.58,15534500.00
aapl,2018-07-18 00:00,191.78,191.80,189.93,187.55,16393400.00
aapl,2018-07-19 00:00,189.69,192.55,189.69,189.00,20286800.00
aapl,2018-07-20 00:00,191.78,192.43,190.17,188.57,20676200.00`
	q, _ := NewQuotesFromCSV(csv)
	//fmt.Println(q)
	if len(q) != 2 {
		t.Error("Invalid length")
	}
	if q[0].Symbol != "spy" {
		t.Error("Invalid symbol")
	}
	if q[0].Close[len(q[0].Close)-1] != 274.26 {
		t.Error("Invalid last value")
	}
	if q[1].Symbol != "aapl" {
		t.Error("Invalid symbol")
	}
	if q[1].Close[len(q[1].Close)-1] != 188.57 {
		t.Error("Invalid last value")
	}
}

// --- Phase 0 regression tests ---

// TestNewQuotesFromCSVOrderPreserved pins first-appearance symbol ordering.
// The previous implementation ranged over a map[string]int while consuming
// rows with a shared counter, so symbols were assigned each other's bars.
// With 8 symbols an accidental pass is ~1/40320, versus ~1/2 in the older
// two-symbol test, which was genuinely flaky on master.
func TestNewQuotesFromCSVOrderPreserved(t *testing.T) {
	syms := []string{"spy", "aapl", "msft", "amzn", "goog", "tsla", "nvda", "meta"}

	var b strings.Builder
	b.WriteString("symbol,datetime,open,high,low,close,volume\n")
	for i, s := range syms {
		// close price encodes the symbol index, so a mis-assignment is visible
		for bar := range 3 {
			fmt.Fprintf(&b, "%s,2018-07-%02d 00:00,1,2,0.5,%d.%02d,100\n",
				s, 12+bar, i+1, bar)
		}
	}

	q, err := NewQuotesFromCSV(strings.TrimRight(b.String(), "\n"))
	if err != nil {
		t.Fatalf("NewQuotesFromCSV: %v", err)
	}
	if len(q) != len(syms) {
		t.Fatalf("got %d quotes, want %d", len(q), len(syms))
	}
	for i, want := range syms {
		if q[i].Symbol != want {
			t.Errorf("quote %d: symbol = %q, want %q", i, q[i].Symbol, want)
		}
		if len(q[i].Close) != 3 {
			t.Fatalf("quote %d (%s): got %d bars, want 3", i, q[i].Symbol, len(q[i].Close))
		}
		// every bar must belong to this symbol, not a neighbour's block
		for bar := range 3 {
			wantClose := float64(i+1) + float64(bar)/100
			if q[i].Close[bar] != wantClose {
				t.Errorf("quote %d (%s) bar %d: close = %v, want %v",
					i, want, bar, q[i].Close[bar], wantClose)
			}
		}
	}
}

// TestValidMarketNoTokenSideEffects: ValidMarket answers "is this a known
// market name" only. It used to also check TIINGO_API_TOKEN and print to
// stdout, so a valid name returned false when credentials were absent.
func TestValidMarketNoTokenSideEffects(t *testing.T) {
	t.Setenv("TIINGO_API_TOKEN", "")

	for _, m := range []string{"etf", "nasdaq", "tiingo-btc", "coinbase"} {
		if !ValidMarket(m) {
			t.Errorf("ValidMarket(%q) = false, want true", m)
		}
	}
	if ValidMarket("definitely-not-a-market") {
		t.Error("ValidMarket(bogus) = true, want false")
	}
	if !MarketRequiresToken("tiingo-btc") {
		t.Error("MarketRequiresToken(tiingo-btc) = false, want true")
	}
	if MarketRequiresToken("nasdaq") {
		t.Error("MarketRequiresToken(nasdaq) = true, want false")
	}
}

// TestNewMarketListTokenError: a tiingo market without a token must report
// the missing credential, not fall through to an HTTP request.
func TestNewMarketListTokenError(t *testing.T) {
	t.Setenv("TIINGO_API_TOKEN", "")

	_, err := NewMarketList("tiingo-btc")
	if err == nil {
		t.Fatal("expected error for tiingo market without token")
	}
	if !strings.Contains(err.Error(), "TIINGO_API_TOKEN") {
		t.Errorf("error = %q, want it to mention TIINGO_API_TOKEN", err)
	}
}

// TestNewMarketListRejectsUnknown: every ValidMarkets entry must resolve to a
// source. "etf" previously passed ValidMarket but had no switch case, leaving
// url == "" and failing with `unsupported protocol scheme ""`.
func TestNewMarketListRejectsUnknown(t *testing.T) {
	_, err := NewMarketList("bogus-market")
	if err == nil {
		t.Fatal("expected error for unknown market")
	}
	if strings.Contains(err.Error(), "unsupported protocol scheme") {
		t.Errorf("unknown market reached the http client: %v", err)
	}
}

// --- Phase 1 characterization tests ---

func testQuote(sym string, bars int) Quote {
	q := NewQuote(sym, bars)
	base := time.Date(2018, 7, 12, 0, 0, 0, 0, time.UTC)
	for i := range bars {
		q.Date[i] = base.AddDate(0, 0, i)
		q.Open[i] = float64(i) + 1.10
		q.High[i] = float64(i) + 2.20
		q.Low[i] = float64(i) + 0.30
		q.Close[i] = float64(i) + 1.50
		q.Volume[i] = float64((i + 1) * 1000)
	}
	return q
}

// TestQuoteCSVRoundTrip: Quote -> CSV -> Quote must preserve every field.
// This is the safety net for replacing the hand-rolled formatter.
func TestQuoteCSVRoundTrip(t *testing.T) {
	orig := testQuote("spy", 5)

	got, err := NewQuoteFromCSV(orig.Symbol, orig.CSV())
	if err != nil {
		t.Fatalf("NewQuoteFromCSV: %v", err)
	}
	if got.Symbol != orig.Symbol {
		t.Errorf("symbol = %q, want %q", got.Symbol, orig.Symbol)
	}
	if len(got.Close) != len(orig.Close) {
		t.Fatalf("bars = %d, want %d", len(got.Close), len(orig.Close))
	}
	for i := range orig.Close {
		if !got.Date[i].Equal(orig.Date[i]) {
			t.Errorf("bar %d date = %v, want %v", i, got.Date[i], orig.Date[i])
		}
		for _, f := range []struct {
			name      string
			got, want float64
		}{
			{"open", got.Open[i], orig.Open[i]},
			{"high", got.High[i], orig.High[i]},
			{"low", got.Low[i], orig.Low[i]},
			{"close", got.Close[i], orig.Close[i]},
			{"volume", got.Volume[i], orig.Volume[i]},
		} {
			if f.got != f.want {
				t.Errorf("bar %d %s = %v, want %v", i, f.name, f.got, f.want)
			}
		}
	}
}

// TestQuotesCSVRoundTrip covers the multi-symbol path.
func TestQuotesCSVRoundTrip(t *testing.T) {
	orig := Quotes{testQuote("spy", 4), testQuote("aapl", 3), testQuote("msft", 5)}

	got, err := NewQuotesFromCSV(strings.TrimRight(orig.CSV(), "\n"))
	if err != nil {
		t.Fatalf("NewQuotesFromCSV: %v", err)
	}
	if len(got) != len(orig) {
		t.Fatalf("quotes = %d, want %d", len(got), len(orig))
	}
	for i := range orig {
		if got[i].Symbol != orig[i].Symbol {
			t.Errorf("quote %d symbol = %q, want %q", i, got[i].Symbol, orig[i].Symbol)
		}
		if len(got[i].Close) != len(orig[i].Close) {
			t.Errorf("quote %d (%s) bars = %d, want %d",
				i, orig[i].Symbol, len(got[i].Close), len(orig[i].Close))
			continue
		}
		for bar := range orig[i].Close {
			if got[i].Close[bar] != orig[i].Close[bar] {
				t.Errorf("quote %d (%s) bar %d close = %v, want %v",
					i, orig[i].Symbol, bar, got[i].Close[bar], orig[i].Close[bar])
			}
		}
	}
}

func TestQuoteJSONRoundTrip(t *testing.T) {
	orig := testQuote("spy", 5)

	got, err := NewQuoteFromJSON(orig.JSON(false))
	if err != nil {
		t.Fatalf("NewQuoteFromJSON: %v", err)
	}
	if got.Symbol != orig.Symbol || len(got.Close) != len(orig.Close) {
		t.Fatalf("got %s/%d bars, want %s/%d",
			got.Symbol, len(got.Close), orig.Symbol, len(orig.Close))
	}
	for i := range orig.Close {
		if got.Close[i] != orig.Close[i] {
			t.Errorf("bar %d close = %v, want %v", i, got.Close[i], orig.Close[i])
		}
	}
}

// TestHighstockValidJSON: the Highstock encoders advertise JSON, so their
// output must parse. Quotes.Highstock writes the `"sym":[` opener inside the
// bar loop but the closing `]` unconditionally, so a symbol with zero bars
// emits a stray bracket.
func TestHighstockValidJSON(t *testing.T) {
	cases := []struct {
		name string
		in   Quotes
	}{
		{"single", Quotes{testQuote("spy", 3)}},
		{"multiple", Quotes{testQuote("spy", 3), testQuote("aapl", 2)}},
		{"empty bars", Quotes{testQuote("spy", 3), testQuote("aapl", 0)}},
		{"all empty", Quotes{testQuote("spy", 0)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.in.Highstock()
			var v map[string]any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Errorf("Quotes.Highstock produced invalid JSON: %v\noutput:\n%s", err, out)
			}
		})
	}
}

func TestQuoteHighstockValidJSON(t *testing.T) {
	for _, bars := range []int{0, 1, 5} {
		out := testQuote("spy", bars).Highstock()
		var v any
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Errorf("Quote.Highstock(%d bars) produced invalid JSON: %v\noutput:\n%s",
				bars, err, out)
		}
	}
}

func TestGetPrecision(t *testing.T) {
	cases := []struct {
		symbol string
		want   int
	}{
		{"spy", 2},
		{"aapl", 2},
		{"btcusd", 8},
		{"BTC-USD", 8},
		{"eth-usd", 8},
		// documents a known false positive: an equity ticker containing "usd"
		{"usdcorp", 8},
	}
	for _, tc := range cases {
		if got := getPrecision(tc.symbol); got != tc.want {
			t.Errorf("getPrecision(%q) = %d, want %d", tc.symbol, got, tc.want)
		}
	}
}

func TestDeleteEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"removes empties", []string{"a", "", "b", "", ""}, []string{"a", "b"}},
		{"all empty", []string{"", ""}, nil},
		{"nothing to remove", []string{"a", "b"}, []string{"a", "b"}},
		{"empty input", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deleteEmpty(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("index %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseDateString pins current behavior, including the sharp edges:
// "" returns now, an unparseable string returns the zero time, and input
// longer than the layout panics.
func TestParseDateString(t *testing.T) {
	if got := ParseDateString("2018-07-12"); !got.Equal(time.Date(2018, 7, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ParseDateString(date) = %v", got)
	}
	if got := ParseDateString("2018"); !got.Equal(time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ParseDateString(year) = %v", got)
	}
	if got := ParseDateString(""); got.IsZero() {
		t.Error("ParseDateString(\"\") should return now, got zero time")
	}
	if got := ParseDateString("not-a-date"); !got.IsZero() {
		t.Errorf("ParseDateString(garbage) = %v, want zero time", got)
	}

	t.Run("overlong input panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for input longer than the layout")
			}
		}()
		_ = ParseDateString("2018-07-12 00:00:00")
	})
}

// --- Phase 3: provider registry, period and format parsing ---

func TestParsePeriod(t *testing.T) {
	cases := []struct {
		in   string
		want Period
	}{
		{"d", Daily}, {"1d", Daily}, {"w", Weekly}, {"1w", Weekly},
		{"m", Monthly}, {"1M", Monthly}, {"1m", Min1}, {"5m", Min5},
		{"1h", Min60}, {"12h", Hour12}, {"3d", Day3},
	}
	for _, tc := range cases {
		got, err := ParsePeriod(tc.in)
		if err != nil {
			t.Errorf("ParsePeriod(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParsePeriod(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// An unknown period must error rather than silently becoming Daily.
	if _, err := ParsePeriod("7y"); err == nil {
		t.Error("ParsePeriod(7y) should fail, not default to Daily")
	}
}

func TestParseFormat(t *testing.T) {
	for _, s := range []string{"csv", "json", "hs", "ami"} {
		if _, err := ParseFormat(s); err != nil {
			t.Errorf("ParseFormat(%q): %v", s, err)
		}
	}
	// An unknown format previously fell through every if/else and wrote
	// nothing while reporting success.
	if _, err := ParseFormat("xlsx"); err == nil {
		t.Error("ParseFormat(xlsx) should fail")
	}
}

func TestProviderRegistry(t *testing.T) {
	want := []string{"coinbase", "tiingo", "tiingo-crypto"}
	got := ProviderNames()
	if len(got) != len(want) {
		t.Fatalf("ProviderNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ProviderNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := DefaultClient.Provider("nope"); err == nil {
		t.Error("Provider(nope) should fail")
	}
}

// Each provider must reject periods it cannot serve, rather than returning
// wrong-resolution data.
func TestProviderPeriodValidation(t *testing.T) {
	cases := []struct {
		source string
		period Period
		ok     bool
	}{
		{"tiingo", Daily, true},
		{"tiingo", Weekly, true},
		{"tiingo", Min5, false},
		{"tiingo-crypto", Min15, true},
		{"tiingo-crypto", Weekly, false},
		{"coinbase", Min5, true},
		{"coinbase", Hour6, false},
	}
	for _, tc := range cases {
		p, err := DefaultClient.Provider(tc.source)
		if err != nil {
			t.Fatalf("Provider(%q): %v", tc.source, err)
		}
		err = CheckPeriod(p, tc.period)
		if tc.ok && err != nil {
			t.Errorf("%s should accept %q: %v", tc.source, tc.period, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s should reject %q", tc.source, tc.period)
		}
	}
}

func TestWriteFileDefaultNames(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	q := testQuote("spy", 2)
	for _, tc := range []struct {
		format Format
		want   string
	}{
		{FormatCSV, "spy.csv"},
		{FormatJSON, "spy.json"},
		{FormatHighstock, "spy.json"},
		{FormatAmibroker, "spy.csv"},
	} {
		if err := q.WriteFile("", tc.format); err != nil {
			t.Fatalf("WriteFile(%q): %v", tc.format, err)
		}
		if _, err := os.Stat(tc.want); err != nil {
			t.Errorf("format %q did not create %s: %v", tc.format, tc.want, err)
		}
	}

	qs := Quotes{q}
	if err := qs.WriteFile("", FormatCSV); err != nil {
		t.Fatalf("Quotes.WriteFile: %v", err)
	}
	if _, err := os.Stat("quotes.csv"); err != nil {
		t.Errorf("Quotes.WriteFile did not create quotes.csv: %v", err)
	}

	if err := q.WriteFile("", Format("bogus")); err == nil {
		t.Error("WriteFile with an invalid format should fail")
	}
}

// WriteFile must produce byte-identical output to the deprecated per-format
// methods it replaces.
func TestWriteFileMatchesLegacyMethods(t *testing.T) {
	q := testQuote("spy", 3)
	qs := Quotes{testQuote("spy", 3), testQuote("aapl", 2)}

	for _, tc := range []struct {
		name   string
		format Format
		legacy string
		modern func() (string, error)
	}{
		{"quote csv", FormatCSV, q.CSV(), func() (string, error) { return q.Encode(FormatCSV) }},
		{"quote json", FormatJSON, q.JSON(false), func() (string, error) { return q.Encode(FormatJSON) }},
		{"quote hs", FormatHighstock, q.Highstock(), func() (string, error) { return q.Encode(FormatHighstock) }},
		{"quote ami", FormatAmibroker, q.Amibroker(), func() (string, error) { return q.Encode(FormatAmibroker) }},
		{"quotes csv", FormatCSV, qs.CSV(), func() (string, error) { return qs.Encode(FormatCSV) }},
		{"quotes json", FormatJSON, qs.JSON(false), func() (string, error) { return qs.Encode(FormatJSON) }},
		{"quotes hs", FormatHighstock, qs.Highstock(), func() (string, error) { return qs.Encode(FormatHighstock) }},
		{"quotes ami", FormatAmibroker, qs.Amibroker(), func() (string, error) { return qs.Encode(FormatAmibroker) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.modern()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if got != tc.legacy {
				t.Errorf("Encode(%q) differs from the legacy method", tc.format)
			}
		})
	}
}

// --- precision: source-declared rather than guessed from the symbol name ---

// TestPrecisionEURPairNotCollapsed is the regression test for the worst case:
// a Coinbase pair quoted in EUR or GBP contains none of "BTC"/"ETH"/"USD", so
// the symbol-name heuristic formatted it to 2 decimals and an entire day of
// sub-euro price action collapsed to a single repeated value.
func TestPrecisionEURPairNotCollapsed(t *testing.T) {
	q := NewQuote("DOGE-EUR", 1)
	q.Precision = PrecisionCrypto
	q.Date[0] = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q.Open[0], q.High[0], q.Low[0], q.Close[0] = 0.0814, 0.0835, 0.0804, 0.0834
	q.Volume[0] = 5731155.2

	csv := q.CSV()
	for _, want := range []string{"0.08140000", "0.08350000", "0.08040000", "0.08340000"} {
		if !strings.Contains(csv, want) {
			t.Errorf("CSV missing %s:\n%s", want, csv)
		}
	}

	// The heuristic alone would have produced 0.08 for all four.
	if getPrecision("DOGE-EUR") != PrecisionEquity {
		t.Fatal("precondition: the symbol heuristic should still guess 2 for DOGE-EUR")
	}
	if strings.Contains(csv, ",0.08,0.08,0.08,0.08,") {
		t.Error("OHLC collapsed to a single value")
	}
}

// A Quote with no Precision set still falls back to the old heuristic, so
// hand-built Quotes behave as before.
func TestPrecisionFallsBackToHeuristic(t *testing.T) {
	cases := []struct {
		symbol string
		want   int
	}{
		{"spy", PrecisionEquity},
		{"btc-usd", PrecisionCrypto},
	}
	for _, tc := range cases {
		q := NewQuote(tc.symbol, 0) // Precision left at zero
		if got := q.precision(); got != tc.want {
			t.Errorf("%s: precision() = %d, want %d", tc.symbol, got, tc.want)
		}
	}

	// An explicit Precision wins over the heuristic.
	q := NewQuote("usdu", 0) // heuristic would say 8: the ticker contains "USD"
	q.Precision = PrecisionEquity
	if got := q.precision(); got != PrecisionEquity {
		t.Errorf("explicit Precision ignored: got %d", got)
	}
}

// Providers must stamp Precision so encoders never have to guess.
func TestProvidersSetPrecision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/crypto/"):
			w.Write([]byte(`[{"ticker":"btcusd","priceData":[{"date":"2024-01-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10}]}]`))
		case strings.Contains(r.URL.Path, "/candles"):
			w.Write([]byte(`[[1704067200,0.0804,0.0835,0.0814,0.0834,5731155.2]]`))
		default:
			w.Write([]byte(tiingoDailyBody))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	now := time.Now()

	// An equity ticker containing "USD" must not be treated as crypto.
	q, err := c.TiingoDaily(context.Background(), "usdu", now, now, Daily, "t")
	if err != nil {
		t.Fatalf("TiingoDaily: %v", err)
	}
	if q.precision() != PrecisionEquity {
		t.Errorf("tiingo daily precision = %d, want %d", q.precision(), PrecisionEquity)
	}

	qc, err := c.TiingoCrypto(context.Background(), "btcusd", now, now, Daily, "t")
	if err != nil {
		t.Fatalf("TiingoCrypto: %v", err)
	}
	if qc.precision() != PrecisionCrypto {
		t.Errorf("tiingo crypto precision = %d, want %d", qc.precision(), PrecisionCrypto)
	}

	qb, err := c.Coinbase(context.Background(), "DOGE-EUR",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Daily)
	if err != nil {
		t.Fatalf("Coinbase: %v", err)
	}
	if qb.precision() != PrecisionCrypto {
		t.Errorf("coinbase precision = %d, want %d", qb.precision(), PrecisionCrypto)
	}
}

// Reading a file back and writing it out again must not silently round it.
func TestPrecisionSurvivesCSVRoundTrip(t *testing.T) {
	orig := "datetime,open,high,low,close,volume\n" +
		"2024-01-01 00:00,0.08140000,0.08350000,0.08040000,0.08340000,5731155.20000000\n"

	q, err := NewQuoteFromCSV("DOGE-EUR", orig)
	if err != nil {
		t.Fatalf("NewQuoteFromCSV: %v", err)
	}
	if q.Precision != 8 {
		t.Errorf("inferred Precision = %d, want 8", q.Precision)
	}
	if got := q.CSV(); got != orig {
		t.Errorf("round trip changed the file:\n got: %q\nwant: %q", got, orig)
	}
}

func TestInferPrecisionFloorsAtTwo(t *testing.T) {
	// Whole numbers must not collapse to zero decimal places.
	csv := "datetime,open,high,low,close,volume\n2024-01-01 00:00,1,2,3,4,5\n"
	q, err := NewQuoteFromCSV("spy", csv)
	if err != nil {
		t.Fatalf("NewQuoteFromCSV: %v", err)
	}
	if q.Precision != PrecisionEquity {
		t.Errorf("Precision = %d, want %d", q.Precision, PrecisionEquity)
	}
	if !strings.Contains(q.CSV(), "1.00,2.00,3.00,4.00") {
		t.Errorf("unexpected formatting: %s", q.CSV())
	}
}

func TestDecimalsIn(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"1", 0}, {"1.5", 1}, {"0.08140000", 8}, {"", 0}, {"12.", 0},
	} {
		if got := decimalsIn(tc.in); got != tc.want {
			t.Errorf("decimalsIn(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// --- timezone: every time this package produces must be UTC ---

// TestCoinbaseCSVRoundTripPreservesInstant is the regression test for the
// write/read asymmetry. Coinbase timestamps came from time.Unix, which returns
// a local time; the CSV format carries no zone, so the parser read them back
// as UTC and every timestamp moved by the local offset.
func TestCoinbaseCSVRoundTripPreservesInstant(t *testing.T) {
	body := `[[1704067200,0.0804,0.0835,0.0814,0.0834,5731155.2]]` // 2024-01-01 00:00 UTC
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	q, err := newTestClient(srv).Coinbase(context.Background(), "btc-eur",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Daily)
	if err != nil {
		t.Fatalf("Coinbase: %v", err)
	}
	if len(q.Date) != 1 {
		t.Fatalf("got %d bars, want 1", len(q.Date))
	}

	want := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if !q.Date[0].Equal(want) {
		t.Errorf("date = %v, want %v", q.Date[0], want)
	}
	if got := q.Date[0].Format("2006-01-02 15:04"); got != "2024-01-01 00:00" {
		t.Errorf("CSV label = %q, want %q (a local-zone value labels it the previous evening)", got, "2024-01-01 00:00")
	}

	back, err := NewQuoteFromCSV("btc-eur", q.CSV())
	if err != nil {
		t.Fatalf("NewQuoteFromCSV: %v", err)
	}
	if shift := back.Date[0].Sub(q.Date[0]); shift != 0 {
		t.Errorf("round trip shifted the instant by %v", shift)
	}
}

// All sources must agree on zone, so a Tiingo file and a Coinbase file label
// the same instant the same way.
func TestAllSourcesProduceUTC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/crypto/"):
			w.Write([]byte(`[{"ticker":"btcusd","priceData":[{"date":"2024-01-01T00:00:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":10}]}]`))
		case strings.Contains(r.URL.Path, "/candles"):
			w.Write([]byte(`[[1704067200,1,2,0.5,1.5,10]]`))
		default:
			w.Write([]byte(`[{"date":"2024-01-01T00:00:00.000Z","adjOpen":1,"adjHigh":2,"adjLow":0.5,"adjClose":1.5,"volume":10}]`))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	daily, err := c.TiingoDaily(context.Background(), "spy", from, to, Daily, "t")
	if err != nil {
		t.Fatalf("TiingoDaily: %v", err)
	}
	crypto, err := c.TiingoCrypto(context.Background(), "btcusd", from, to, Daily, "t")
	if err != nil {
		t.Fatalf("TiingoCrypto: %v", err)
	}
	cb, err := c.Coinbase(context.Background(), "btc-usd", from, to, Daily)
	if err != nil {
		t.Fatalf("Coinbase: %v", err)
	}

	for _, tc := range []struct {
		name string
		q    Quote
	}{{"tiingo daily", daily}, {"tiingo crypto", crypto}, {"coinbase", cb}} {
		if len(tc.q.Date) == 0 {
			t.Fatalf("%s returned no bars", tc.name)
		}
		if loc := tc.q.Date[0].Location(); loc != time.UTC {
			t.Errorf("%s: location = %v, want UTC", tc.name, loc)
		}
		if got := tc.q.Date[0].Format("2006-01-02 15:04"); got != "2024-01-01 00:00" {
			t.Errorf("%s: label = %q, want 2024-01-01 00:00", tc.name, got)
		}
	}
}

// ParseDateString returned a local time for "" and a UTC time for everything
// else, so the two branches could format to different dates near midnight.
func TestParseDateStringUTCConsistency(t *testing.T) {
	if loc := ParseDateString("").Location(); loc != time.UTC {
		t.Errorf(`ParseDateString("").Location() = %v, want UTC`, loc)
	}
	if loc := ParseDateString("2024-01-01").Location(); loc != time.UTC {
		t.Errorf("ParseDateString(date).Location() = %v, want UTC", loc)
	}
	// The implicit "now" and an explicit today must name the same day.
	now := ParseDateString("")
	explicit := ParseDateString(now.Format("2006-01-02"))
	if now.Format("2006-01-02") != explicit.Format("2006-01-02") {
		t.Errorf("implicit now (%s) and explicit date (%s) disagree",
			now.Format("2006-01-02"), explicit.Format("2006-01-02"))
	}
}

// --- Scanner error handling ---
//
// bufio.Scanner ends its loop silently on error, so an unreadable row used to
// look like end-of-file. In update mode that is destructive rather than merely
// wrong: the rewrite drops every row past the error and then renames the
// truncated .tmp over the user's original file. An oversized line is the
// reproducible trigger (bufio.ErrTooLong), but a mid-read I/O error behaves the
// same way.

// oversizedLine exceeds bufio's default token limit, so scanning it fails.
func oversizedLine() string { return strings.Repeat("x", bufio.MaxScanTokenSize+1) }

// assertUntouched fails if path no longer matches want, or if a .tmp was left.
func assertUntouched(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("file was modified by a failed update\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("left a partial %s.tmp behind", path)
	}
}

func TestUpdateSingle_ScanErrorAbortsBeforeFetch(t *testing.T) {
	initial := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,1,1,1,10,100",
		oversizedLine(),
		"2025-01-02 00:00,1,1,1,11,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "spy.csv", initial)

	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		t.Errorf("fetched %s; the read failure should abort before any request", symbol)
		return nil, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	err := UpdateFileTiingo(path, "token", 2, false, 1, time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected an error from the unreadable line, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want it to wrap bufio.ErrTooLong", err)
	}
	assertUntouched(t, path, initial)
}

// The second pass reads the file again after the fetch, so a file that becomes
// unreadable in between must abort the rewrite rather than rename a partial
// .tmp over the original.
func TestUpdateSingle_ScanErrorDuringRewriteDoesNotTruncate(t *testing.T) {
	initial := strings.Join([]string{
		"datetime,open,high,low,close,volume",
		"2025-01-01 00:00,1,1,1,10,100",
		"2025-01-02 00:00,1,1,1,11,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "spy.csv", initial)
	corrupted := initial + oversizedLine() + "\n"

	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		if err := os.WriteFile(path, []byte(corrupted), 0644); err != nil {
			t.Fatalf("corrupt file: %v", err)
		}
		return []tquoteRaw{{
			Date: "2025-01-03", AdjOpen: 2, AdjHigh: 3, AdjLow: 1,
			AdjClose: 43, Volume: 200, SplitFactor: 1.0,
		}}, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	err := UpdateFileTiingo(path, "token", 2, false, 1, time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected an error from the unreadable line, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want it to wrap bufio.ErrTooLong", err)
	}
	assertUntouched(t, path, corrupted)
}

func TestUpdateMulti_ScanErrorAbortsBeforeFetch(t *testing.T) {
	initial := strings.Join([]string{
		"symbol,datetime,open,high,low,close,volume",
		"aaa,2025-01-01 00:00,1,1,1,10,100",
		oversizedLine(),
		"bbb,2025-01-01 00:00,1,1,1,20,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "multi.csv", initial)

	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		t.Errorf("fetched %s; the read failure should abort before any request", symbol)
		return nil, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	err := UpdateFileTiingo(path, "token", 2, false, 1, time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected an error from the unreadable line, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want it to wrap bufio.ErrTooLong", err)
	}
	assertUntouched(t, path, initial)
}

func TestUpdateMulti_ScanErrorDuringRewriteDoesNotTruncate(t *testing.T) {
	initial := strings.Join([]string{
		"symbol,datetime,open,high,low,close,volume",
		"aaa,2025-01-01 00:00,1,1,1,10,100",
		"aaa,2025-01-02 00:00,1,1,1,11,100",
		"",
	}, "\n")
	path, _ := writeTempFile(t, "multi.csv", initial)
	corrupted := initial + oversizedLine() + "\n"

	prev := tiingoFetch
	tiingoFetch = func(symbol string, from, to time.Time, token string) ([]tquoteRaw, error) {
		if err := os.WriteFile(path, []byte(corrupted), 0644); err != nil {
			t.Errorf("corrupt file: %v", err)
		}
		return []tquoteRaw{{
			Date: "2025-01-03", AdjOpen: 2, AdjHigh: 3, AdjLow: 1,
			AdjClose: 43, Volume: 200, SplitFactor: 1.0,
		}}, nil
	}
	defer func() { tiingoFetch = prev }()

	Delay = 0
	err := UpdateFileTiingo(path, "token", 2, false, 1, time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected an error from the unreadable line, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want it to wrap bufio.ErrTooLong", err)
	}
	assertUntouched(t, path, corrupted)
}
