package quote

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Format identifies an output encoding.
type Format string

// Supported output formats.
const (
	// FormatCSV - comma separated values
	FormatCSV Format = "csv"
	// FormatJSON - json
	FormatJSON Format = "json"
	// FormatHighstock - Highstock json
	FormatHighstock Format = "hs"
	// FormatAmibroker - Amibroker csv
	FormatAmibroker Format = "ami"
)

// formatExt is the default file extension for each format.
var formatExt = map[Format]string{
	FormatCSV:       ".csv",
	FormatJSON:      ".json",
	FormatHighstock: ".json",
	FormatAmibroker: ".csv",
}

// ParseFormat converts a user-supplied format string to a Format.
//
// The CLI previously matched formats with an if/else chain that had no final
// else, so an unrecognized -format silently wrote nothing and reported success.
func ParseFormat(s string) (Format, error) {
	f := Format(s)
	if _, ok := formatExt[f]; ok {
		return f, nil
	}
	return "", fmt.Errorf("invalid format %q, must be one of: %s", s, strings.Join(FormatNames(), ", "))
}

// FormatNames lists the supported format names in sorted order.
func FormatNames() []string {
	names := make([]string, 0, len(formatExt))
	for f := range formatExt {
		names = append(names, string(f))
	}
	sort.Strings(names)
	return names
}

// defaultFilename builds a filename for a format when none was supplied.
func defaultFilename(base string, f Format) string {
	if base == "" {
		base = "quote"
	}
	return base + formatExt[f]
}

// Encode renders the Quote in the given format.
func (q Quote) Encode(f Format) (string, error) {
	switch f {
	case FormatCSV:
		return q.CSV(), nil
	case FormatJSON:
		return q.JSON(false), nil
	case FormatHighstock:
		return q.Highstock(), nil
	case FormatAmibroker:
		return q.Amibroker(), nil
	}
	return "", fmt.Errorf("invalid format %q", f)
}

// WriteFile writes the Quote to filename in the given format. An empty
// filename derives one from the symbol.
//
// This replaces the per-format WriteCSV/WriteJSON/WriteHighstock/WriteAmibroker
// methods, each of which repeated the same default-filename-then-WriteFile body.
func (q Quote) WriteFile(filename string, f Format) error {
	s, err := q.Encode(f)
	if err != nil {
		return err
	}
	if filename == "" {
		filename = defaultFilename(q.Symbol, f)
	}
	return os.WriteFile(filename, []byte(s), 0644)
}

// Encode renders the Quotes in the given format.
func (q Quotes) Encode(f Format) (string, error) {
	switch f {
	case FormatCSV:
		return q.CSV(), nil
	case FormatJSON:
		return q.JSON(false), nil
	case FormatHighstock:
		return q.Highstock(), nil
	case FormatAmibroker:
		return q.Amibroker(), nil
	}
	return "", fmt.Errorf("invalid format %q", f)
}

// WriteFile writes the Quotes to filename in the given format. An empty
// filename defaults to "quotes" with the format's extension.
func (q Quotes) WriteFile(filename string, f Format) error {
	s, err := q.Encode(f)
	if err != nil {
		return err
	}
	if filename == "" {
		filename = defaultFilename("quotes", f)
	}
	return os.WriteFile(filename, []byte(s), 0644)
}
