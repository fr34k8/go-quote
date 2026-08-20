package quote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Update mode: in-place incremental updates of existing Tiingo CSV files.

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
	// Update mode is Tiingo daily only, which is equities.
	precision := PrecisionEquity
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
		end = time.Now().UTC()
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
		end = time.Now().UTC()
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
