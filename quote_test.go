package quote

import (
    "fmt"
    "os"
    "path/filepath"
    "reflect"
    "runtime"
    "strings"
    "testing"
    "time"
)

// assert fails the test if the condition is false.
func assert(t *testing.T, condition bool, msg string, v ...interface{}) {
	if !condition {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("%s:%d: "+msg+"\n", append([]interface{}{filepath.Base(file), line}, v...)...)
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
func equals(t *testing.T, exp, act interface{}) {
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
    if err != nil { panic(err) }
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
