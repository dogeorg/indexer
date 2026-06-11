package spec

import "context"

// CoreRequestClient is the generic JSON-RPC shape exposed by dogewalker's Core client.
type CoreRequestClient interface {
	Request(ctx context.Context, method string, params []any, result any) (int, error)
}
