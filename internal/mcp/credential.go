package mcp

import (
	"context"
	"net/http"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/credctx"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// WithCredential stores the resolved Portal API JWT in the context. It is the
// single way identity is injected for the current request. It is a thin
// delegate to the shared credctx leaf package so internal/mcp/core/transfer
// (which cannot import internal/mcp without an import cycle) and internal/mcp
// read and write the same credential from a context.
func WithCredential(ctx context.Context, jwt string) context.Context {
	return credctx.With(ctx, jwt)
}

// CredentialFromContext returns the Portal API JWT for the current request, or
// "" when none is set (unauthenticated or CLI/local mode, where services fall
// back to the config token). It is a thin delegate to credctx.From.
func CredentialFromContext(ctx context.Context) string {
	return credctx.From(ctx)
}

// credentialMiddleware resolves the Portal API JWT once per request and stores
// it in the request context, so every authenticated handler downstream reads a
// single consistent identity. It is installed only when a CredentialResolver
// is present (hosted/Portal-embedded HTTP path); without a resolver it is a
// pass-through, preserving the CLI/local config-token fallback.
func credentialMiddleware(resolver CredentialResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if resolver != nil {
			if tok, err := resolver.TokenForRequest(ctx); err == nil && tok != "" {
				ctx = WithCredential(ctx, tok)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HTTPHandler wraps an assembled server as a streamable-HTTP handler (RFC
// Streamable HTTP transport). When a CredentialResolver is supplied (hosted
// path), the streamable handler is wrapped with credentialMiddleware so the
// per-request Portal API JWT is resolved once and carried on the context.
// disableLocalhostProtection is required when the handler is served behind a
// proxy/tunnel that presents a non-loopback Origin.
func HTTPHandler(srv *sdk.Server, resolver CredentialResolver, disableLocalhostProtection bool) http.Handler {
	handler := sdk.NewStreamableHandler(srv, disableLocalhostProtection)
	if resolver != nil {
		handler = credentialMiddleware(resolver, handler)
	}
	return handler
}
