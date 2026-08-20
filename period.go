package quote

import (
	"fmt"
	"sort"
	"strings"
)

// periodNames maps the user-facing period spelling to a Period constant.
// Several spellings are accepted for the same period ("d" and "1d").
//
// Note the Period constant *values* are inconsistent for historical reasons -
// Min1 is "60" and Min5 is "300" (Coinbase granularity seconds) while Min3 is
// "3m" and Hour2 is "2h". Those values are part of the public API and may have
// been persisted by callers, so they are deliberately left alone; this table is
// what gives users a consistent vocabulary.
var periodNames = map[string]Period{
	"1m":  Min1,
	"3m":  Min3,
	"5m":  Min5,
	"15m": Min15,
	"30m": Min30,
	"1h":  Min60,
	"2h":  Hour2,
	"4h":  Hour4,
	"6h":  Hour6,
	"8h":  Hour8,
	"12h": Hour12,
	"d":   Daily,
	"1d":  Daily,
	"3d":  Day3,
	"w":   Weekly,
	"1w":  Weekly,
	"m":   Monthly,
	"1M":  Monthly,
}

// periodStrings is the canonical spelling for each Period, for error messages.
var periodStrings = map[Period]string{
	Min1:    "1m",
	Min3:    "3m",
	Min5:    "5m",
	Min15:   "15m",
	Min30:   "30m",
	Min60:   "1h",
	Hour2:   "2h",
	Hour4:   "4h",
	Hour6:   "6h",
	Hour8:   "8h",
	Hour12:  "12h",
	Daily:   "d",
	Day3:    "3d",
	Weekly:  "w",
	Monthly: "m",
}

// ParsePeriod converts a user-supplied period string to a Period.
//
// Unlike the CLI helper it replaces, an unrecognized period is an error rather
// than a silent fallback to Daily - which previously meant asking for "6h" from
// Coinbase quietly returned daily bars.
func ParsePeriod(s string) (Period, error) {
	if p, ok := periodNames[s]; ok {
		return p, nil
	}
	return "", fmt.Errorf("invalid period %q, must be one of: %s", s, strings.Join(PeriodNames(), ", "))
}

// PeriodString returns the canonical spelling of p.
func PeriodString(p Period) string {
	if s, ok := periodStrings[p]; ok {
		return s
	}
	return string(p)
}

// PeriodNames lists the accepted period spellings, shortest first.
func PeriodNames() []string {
	names := make([]string, 0, len(periodNames))
	for n := range periodNames {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) < len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}
