// Package credctx is the neutral leaf package that owns the context key under
// which the per-request Portal API JWT is carried. It lives here rather than in
// internal/mcp so that internal/mcp/core/transfer (which internal/mcp already
// imports, and therefore cannot re-import internal/mcp) and internal/mcp can
// both read/write the same credential from a context without an import cycle.
package credctx

import "context"

type key struct{}

// With stores the Portal API JWT in ctx under the shared unexported key.
func With(ctx context.Context, jwt string) context.Context {
	return context.WithValue(ctx, key{}, jwt)
}

// From returns the Portal API JWT stored on ctx, or "" when absent.
func From(ctx context.Context) string {
	if v, ok := ctx.Value(key{}).(string); ok {
		return v
	}
	return ""
}
