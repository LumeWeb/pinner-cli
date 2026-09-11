package mcpembed

import "go.lumeweb.com/pinner/mcp/hosted"

// CredentialResolver resolves the Portal API token for the authenticated
// principal of the current request. It is the seam that lets a hosted embed
// route the MCP OAuth backend to the Portal's own OAuth library/IdP (which has
// already validated the caller and established a user) instead of forcing the
// CLI's config-token assumptions.
//
// The CLI/local MCP server reads the bearer token from the pinner config. A
// hosted server supplies an implementation that maps the Portal-authenticated
// user (extracted by Portal middleware) onto a Portal API JWT.
//
// It aliases the shared SDK-independent hosted-construction contract
// (go.lumeweb.com/pinner/mcp/hosted), so the CLI's embed surface and a hosted
// composition root bind to the same seam. TokenForRequest returns
// assembly.ErrNotAuthenticated when there is no authenticated caller.
type CredentialResolver = hosted.CredentialResolver

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
//
// It aliases the shared contract from go.lumeweb.com/pinner/mcp/hosted.
type IdentifiableCredentialResolver = hosted.IdentifiableCredentialResolver

// OAuthHandler protects the embedded MCP HTTP endpoint with OAuth. It is the
// surface-agnostic seam between the MCP implementation and an authorization
// server:
//
//   - CLI mode: implemented by the CLI's own OAuth AS (go.lumeweb.com/oauth,
//     login page, dynamic client registration).
//   - Hosted mode: implemented by the Portal plugin, which delegates to the
//     Portal's OAuthProviderService (ValidateAccessToken, RFC 8414/9728) — the
//     embedded MCP server never needs to know OAuth exists.
//
// It aliases the shared contract from go.lumeweb.com/pinner/mcp/hosted.
type OAuthHandler = hosted.OAuthHandler
