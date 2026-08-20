package quote

import "fmt"

// Error types returned by this package.

// SymbolNotFoundError indicates that a symbol was not found in the data source.
// This error type allows callers to distinguish "not found" from other errors.
type SymbolNotFoundError struct {
	Symbol string
}

func (e *SymbolNotFoundError) Error() string {
	return fmt.Sprintf("symbol '%s' not found", e.Symbol)
}
