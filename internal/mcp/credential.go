package mcp

import (
	"context"
	"errors"
	"net/http"

	"go.lumeweb.com/mcpplane/credctx"
	"go.lumeweb.com/mcpplane/sdk"
)

// ErrCredentialsNotConfigured is the sentinel a CredentialResolver returns
// when it has NO credential source configured (yet) — as opposed to
// ErrNotAuthenticated, which means a source exists but the request carries no
// identity. The credential middleware treats it as "no resolver in force":
// the request passes through unauthenticated (services fall back to the
// config token) exactly as it would when no resolver was installed at all.
// Any other error (or a blank token) keeps failing closed with a 401.
var ErrCredentialsNotConfigured = errors.New("no credential source is configured")

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
//
// FAIL CLOSED: when a resolver is configured, a resolver error or a blank
// resolved token rejects the request with 401 before it reaches the MCP
// handler. It must never fall through unauthenticated — the embedded catalog
// ops and transfer services fall back to the config default token when no
// per-request credential is injected, so letting the request continue would
// dispatch an unrecognized (or attacker) caller under the deployment's own
// shared credential. The resolver is the ONLY identity source on the hosted
// path: identity is established by the fronting OAuth/Portal middleware, not
// by local config.
func credentialMiddleware(resolver CredentialResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolver == nil {
			// CLI/local path: no resolver, no injection, request proceeds so
			// services fall back to the config token as they always have.
			next.ServeHTTP(w, r)
			return
		}
		tok, err := resolver.TokenForRequest(r.Context())
		if errors.Is(err, ErrCredentialsNotConfigured) {
			// No credential source is in force for this embed (e.g. a
			// discovery resolver before its factory has surfaced one): behave
			// exactly like an absent middleware — pass through so services
			// fall back to the config token. This is not an auth failure and
			// must neither grant nor deny an identity.
			next.ServeHTTP(w, r)
			return
		}
		if err != nil || tok == "" {
			// Fail closed: no usable per-request identity means the request is
			// unauthenticated, regardless of local config credentials.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","message":"no authenticated user for this request"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithCredential(r.Context(), tok)))
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
