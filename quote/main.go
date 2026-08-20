/*
Package quote is free quote downloader library and cli

Downloads historical price quotes from Tiingo and Coinbase

Copyright 2025 Mark Chenoweth
Licensed under terms of MIT license

*/

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/markcheno/go-quote"
)

var usage = `Usage:
  quote -h | -help
  quote -v | -version
  quote [-outfile=<outputFile>] <market>
  quote [-years=<years>|(-start=<datestr> [-end=<datestr>])] [options] [-infile=<filename>|<symbol> ...]
  quote -update=<path> [-end=<datestr>] [-backfill-days=<n>] [-full-redownload-on-ca] [-concurrency=<n>] [-token=<tiingo_token>]

Options:
  -h -help             show help
  -v -version          show version
  -years=<years>       number of years to download [default=5]
  -start=<datestr>     yyyy[-[mm-[dd]]]
  -end=<datestr>       yyyy[-[mm-[dd]]] [default=today]
  -markets=<list>      list of valid markets to download (comma separated)
  -infile=<filename>   list of symbols to download
  -outfile=<filename>  output filename
  -period=<period>     1m|3m|5m|15m|30m|1h|2h|4h|6h|8h|12h|d|3d|w|m [default=d]
  -source=<source>     tiingo|tiingo-crypto|coinbase|binance [default=tiingo]
  -token=<tiingo_tok>  tingo api token [default=TIINGO_API_TOKEN]
  -format=<format>     (csv|json|hs|ami) [default=csv]
  -all=<bool>          all in one file (true|false) [default=false]
  -log=<dest>          filename|stdout|stderr|discard [default=stdout]
  -delay=<ms>          delay in milliseconds between quote requests
  -update=<path>       update an existing Tiingo CSV (single or -all multi) in place
  -backfill-days=<n>   days of overlap to rewrite [default=10]
  -full-redownload-on-ca  if splits/dividends detected, fully redownload the symbol block
  -concurrency=<n>     concurrent symbol fetches in update mode [default=1]

Note: not all periods work with all sources

Valid markets:
etf,nasdaq,nasdaq100,amex,nyse,megacap,largecap,midcap,smallcap,microcap,nanocap,
telecommunications,health_care,finance,real_estate,consumer_discretionary,
consumer_staples,industrials,basic_materials,energy,utilities,technology
coinbase,tiingo-usd,tiingo-btc,tiingo-eth
binance-usdt,binance-usdc,binance-btc,binance-eth
`

const (
	version    = "0.4"
	dateFormat = "2006-01-02"
)

type quoteflags struct {
	years   int
	delay   int
	start   string
	end     string
	period  string
	source  string
	token   string
	markets string
	infile  string
	outfile string
	format  string
	log     string
	all     bool
	// update mode
	updatePath     string
	backfillDays   int
	fullRedownload bool
	concurrency    int
	version        bool
}

func check(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n\n", e)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
}

// checkFlags validates source, period, format, and credentials.
//
// This used to be a hand-maintained set of if/else chains, including a
// 12-clause boolean for tiingo-crypto whose error message listed values the
// condition did not accept ('1d', '3d', '1w', '1M') and said "invalid source"
// when it meant period. The source and period vocabularies now come from the
// provider registry, so they cannot drift from what the code actually accepts.
func checkFlags(flags quoteflags) error {
	provider, err := quote.DefaultClient.Provider(flags.source)
	if err != nil {
		return err
	}

	period, err := quote.ParsePeriod(flags.period)
	if err != nil {
		return err
	}
	if err := quote.CheckPeriod(provider, period); err != nil {
		return err
	}

	if _, err := quote.ParseFormat(flags.format); err != nil {
		return err
	}

	if strings.HasPrefix(flags.source, "tiingo") && flags.token == "" {
		return fmt.Errorf("missing token for %s, must be passed or TIINGO_API_TOKEN must be set", flags.source)
	}

	return nil
}

func setOutput(flags quoteflags) error {
	var err error
	if flags.log == "stdout" {
		quote.Log.SetOutput(os.Stdout)
	} else if flags.log == "stderr" {
		quote.Log.SetOutput(os.Stderr)
	} else if flags.log == "discard" {
		quote.Log.SetOutput(io.Discard)
	} else {
		var f *os.File
		f, err = os.OpenFile(flags.log, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return err
		}
		// No defer Close here: the logger writes to this handle for the rest
		// of the run, and the process owns it until exit. Closing on return
		// would send every subsequent log line to a closed fd.
		quote.Log.SetOutput(f)
	}
	return err
}

// func getSymbols(flags quoteflags, args []string) ([]string, error) {
// 	var err error
// 	var symbols []string
// 	if flags.infile != "" {
// 		symbols, err = quote.NewSymbolsFromFile(flags.infile)
// 		if err != nil {
// 			return symbols, err
// 		}
// 	} else {
// 		symbols = args
// 	}
// 	// make sure we found some symbols
// 	if len(symbols) == 0 {
// 		return symbols, fmt.Errorf("no symbols specified")
// 	}
// 	// validate outfileFlag
// 	if len(symbols) > 1 && flags.outfile != "" && !flags.all {
// 		return symbols, fmt.Errorf("outfile not valid with multiple symbols\nuse -all=true")
// 	}
// 	return symbols, nil
// }

func getSymbols(flags quoteflags, args []string) ([]string, error) {
	var err error
	var symbols []string

	if flags.infile != "" {
		// Check if infile contains wildcard characters
		if strings.Contains(flags.infile, "*") || strings.Contains(flags.infile, "?") {
			// Find all matching files
			matches, err := filepath.Glob(flags.infile)
			if err != nil {
				return nil, fmt.Errorf("error processing wildcard pattern: %v", err)
			}

			if len(matches) == 0 {
				return nil, fmt.Errorf("no files match pattern: %s", flags.infile)
			}

			// Read symbols from all matching files
			for _, file := range matches {
				fileSymbols, err := quote.NewSymbolsFromFile(file)
				if err != nil {
					return nil, fmt.Errorf("error reading symbols from %s: %v", file, err)
				}
				symbols = append(symbols, fileSymbols...)
			}
		} else {
			// Regular file handling
			symbols, err = quote.NewSymbolsFromFile(flags.infile)
			if err != nil {
				return symbols, err
			}
		}
	} else if flags.markets != "" {

		markets := strings.SplitSeq(flags.markets, ",")
		for cmd := range markets {
			if !quote.ValidMarket(cmd) {
				return symbols, fmt.Errorf("invalid market specified: %s", cmd)
			}
			file := cmd + ".csv"
			if err := quote.NewMarketFile(cmd, file); err != nil {
				return symbols, fmt.Errorf("error downloading market %s: %v", cmd, err)
			}
			fileSymbols, err := quote.NewSymbolsFromFile(file)
			if err != nil {
				return nil, fmt.Errorf("error reading symbols from %s: %v", file, err)
			}
			symbols = append(symbols, fileSymbols...)
		}
		return symbols, nil
	} else {
		symbols = args
	}

	// make sure we found some symbols
	if len(symbols) == 0 {
		return symbols, fmt.Errorf("no symbols specified")
	}

	// validate outfileFlag
	if len(symbols) > 1 && flags.outfile != "" && !flags.all {
		return symbols, fmt.Errorf("outfile not valid with multiple symbols\nuse -all=true")
	}

	return symbols, nil
}

// resolved holds the validated, parsed form of the CLI flags. Parsing happens
// once here instead of being re-derived in each output path.
type resolved struct {
	provider quote.Provider
	period   quote.Period
	format   quote.Format
	from     time.Time
	to       time.Time
}

func resolve(flags quoteflags) (*resolved, error) {
	provider, err := quote.DefaultClient.Provider(flags.source)
	if err != nil {
		return nil, err
	}

	period, err := quote.ParsePeriod(flags.period)
	if err != nil {
		return nil, err
	}
	if err := quote.CheckPeriod(provider, period); err != nil {
		return nil, err
	}

	format, err := quote.ParseFormat(flags.format)
	if err != nil {
		return nil, err
	}

	from, to := getTimes(flags)
	return &resolved{provider: provider, period: period, format: format, from: from, to: to}, nil
}

func getTimes(flags quoteflags) (time.Time, time.Time) {
	// determine start/end times
	to := quote.ParseDateString(flags.end)
	var from time.Time
	if flags.start != "" {
		from = quote.ParseDateString(flags.start)
	} else { // use years
		from = to.AddDate(-flags.years, 0, 0)
	}
	return from, to
}

func outputAll(symbols []string, flags quoteflags) error {
	r, err := resolve(flags)
	if err != nil {
		return err
	}

	req := quote.Request{From: r.from, To: r.to, Period: r.period, Token: flags.token}
	quotes, err := quote.DefaultClient.FetchSymbols(context.Background(), r.provider, symbols, req)
	if err != nil {
		return err
	}
	return quotes.WriteFile(flags.outfile, r.format)
}

func outputIndividual(symbols []string, flags quoteflags) error {
	r, err := resolve(flags)
	if err != nil {
		return err
	}

	ctx := context.Background()
	for i, sym := range symbols {
		if i > 0 {
			time.Sleep(quote.Delay * time.Millisecond)
		}
		req := quote.Request{Symbol: sym, From: r.from, To: r.to, Period: r.period, Token: flags.token}
		q, err := r.provider.Fetch(ctx, req)
		if err != nil {
			// Previously the fetch error was discarded and an empty file was
			// written for the symbol.
			fmt.Fprintf(os.Stderr, "error downloading %s: %v\n", sym, err)
			continue
		}
		if err := q.WriteFile(flags.outfile, r.format); err != nil {
			fmt.Fprintf(os.Stderr, "error writing file for %s: %v\n", sym, err)
		}
	}
	return nil
}

func handleCommand(symbols []string, flags quoteflags) bool {

	if flags.markets != "" {
		return false
	}

	// handle market special commands
	for _, cmd := range symbols {
		if !quote.ValidMarket(cmd) {
			return false
		}
		check(quote.NewMarketFile(cmd, flags.outfile))
	}
	return true
}

func main() {

	var err error
	var symbols []string
	var flags quoteflags

	flag.IntVar(&flags.years, "years", 5, "number of years to download")
	flag.IntVar(&flags.delay, "delay", 100, "milliseconds to delay between requests")
	flag.StringVar(&flags.start, "start", "", "start date (yyyy[-mm[-dd]])")
	flag.StringVar(&flags.end, "end", "", "end date (yyyy[-mm[-dd]])")
	flag.StringVar(&flags.period, "period", "d", "1m|3m|5m|15m|30m|1h|2h|4h|6h|8h|12h|d|3d|w|m")
	flag.StringVar(&flags.source, "source", "tiingo", "tiingo|tiingo-crypto|coinbase|binance")
	flag.StringVar(&flags.token, "token", os.Getenv("TIINGO_API_TOKEN"), "tiingo api token")
	flag.StringVar(&flags.infile, "infile", "", "input filename")
	flag.StringVar(&flags.outfile, "outfile", "", "output filename")
	flag.StringVar(&flags.markets, "markets", "", "list of valid markets (comma separated)")
	flag.StringVar(&flags.format, "format", "csv", "csv|json|hs|ami")
	flag.StringVar(&flags.log, "log", "stdout", "<filename>|stdout")
	flag.BoolVar(&flags.all, "all", false, "all output in one file")
	flag.BoolVar(&flags.version, "v", false, "show version")
	flag.BoolVar(&flags.version, "version", false, "show version")
	// update flags
	flag.StringVar(&flags.updatePath, "update", "", "update an existing Tiingo CSV in place")
	flag.IntVar(&flags.backfillDays, "backfill-days", 10, "days of overlap to rewrite for splits/dividends")
	flag.BoolVar(&flags.fullRedownload, "full-redownload-on-ca", false, "if splits/dividends detected, fully redownload that symbol block")
	flag.IntVar(&flags.concurrency, "concurrency", 1, "number of concurrent symbol fetches in update mode")
	// -h/-help are documented in usage; without this they print Go's
	// autogenerated flag dump instead.
	flag.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	flag.Parse()

	if flags.version {
		fmt.Println(version)
		os.Exit(0)
	}

	quote.Delay = time.Duration(flags.delay)

	err = setOutput(flags)
	check(err)

	err = checkFlags(flags)
	check(err)

	// Update mode (Tiingo only)
	if flags.updatePath != "" {
		if flags.source != "tiingo" { // only tiingo supported for now
			fmt.Println("update mode currently supports -source=tiingo only")
			os.Exit(1)
		}
		if flags.token == "" {
			fmt.Println("update mode requires TIINGO_API_TOKEN or -token")
			os.Exit(1)
		}
		var endTime time.Time
		if strings.TrimSpace(flags.end) != "" {
			endTime = quote.ParseDateString(flags.end)
		} else {
			endTime = time.Now().UTC()
		}
		if err := quote.UpdateFileTiingo(flags.updatePath, flags.token, flags.backfillDays, flags.fullRedownload, flags.concurrency, endTime); err != nil {
			fmt.Printf("Error updating %s: %v\n", flags.updatePath, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	symbols, err = getSymbols(flags, flag.Args())
	check(err)

	// check for and handle special commands
	if handleCommand(symbols, flags) {
		os.Exit(0)
	}

	//fmt.Println("Downloading quotes for", len(symbols), "symbols")

	// main output
	if flags.all {
		outputAll(symbols, flags)
	} else {
		outputIndividual(symbols, flags)
	}
}
