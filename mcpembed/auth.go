package mcpembed

import (
	"context"
	"net/http"
)

// CredentialResolver resolves the Portal API token for the authenticated
// principal of the current request. It is the seam that lets a hosted embed
// route the MCP OAuth backend to the Portal's own OAuth library/IdP (which has
// already validated the caller and established a user) instead of forcing the
// CLI's config-token assumptions.
//
// The CLI/local MCP server reads the bearer token from the pinner config. A
// hosted server supplies an implementation that maps the Portal-authenticated
// user (extracted by Portal middleware) onto a Portal API JWT.
type CredentialResolver interface {
	// TokenForRequest returns the Portal API token for the currently
	// authenticated request, or an error (ErrNotAuthenticated) when there is
	// none.
	TokenForRequest(ctx context.Context) (string, error)
}

// IdentifiableCredentialResolver is an optional interface a CredentialResolver
// may implement to expose a STABLE identity for its underlying credential
// source. Go cannot compare closures/functions (reflect: funcs are
// non-comparable), so mcpembed can never prove — from the values alone — that
// the Options.CredentialResolver closure and the CatalogDeps bundle's resolver
// closure are the same wired resolver. An implementation whose identity method
// returns an equal value for the same underlying source (e.g. a user-scoped
// resolver instance ID, constant per closure) opts in to that proof: wiring
// THE SAME such closure to both Options and the bundle bundle factory is then
// accepted instead of rejected as a conflict. Two implementations whose
// identities DISAGREE are still a construction wiring conflict, and any
// resolver that proves neither value-equality nor a shared identity fails
// construction closed, exactly as before.
type IdentifiableCredentialResolver interface {
	// ResolverIdentity returns a stable identifier for the resolver's
	// UNDERLYING credential source, not per invocation: two closures wrapping
	// the same credential source must return the same string (and different
	// sources must not). Deterministic across calls on one value.
	ResolverIdentity() string
}

// OAuthHandler protects the embedded MCP HTTP endpoint with OAuth. It is the
// surface-agnostic seam between the MCP implementation and an authorization
// server:
//
//   - CLI mode: implemented by the CLI's own OAuth AS (go.lumeweb.com/oauth,
//     login page, dynamic client registration).
//   - Hosted mode: implemented by the Portal plugin, which delegates to the
//     Portal's OAuthProviderService (ValidateAccessToken, RFC 8414/9728) — the
//     embedded MCP server never needs to know OAuth exists.
type OAuthHandler interface {
	// WrapHTTP wraps the /mcp streamable-HTTP handler with OAuth enforcement
	// (validate Authorization: Bearer, emit 401 + WWW-Authenticate pointing at
	// the protected-resource metadata when invalid). next is the authenticated-
	// session downstream handler.
	WrapHTTP(next http.Handler) http.Handler
}
