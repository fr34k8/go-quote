package quote

import "context"

// FetchAll drives a fetch function across a list of symbols.

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
