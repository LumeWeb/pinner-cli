package mcpembed

import (
	"net/http"

	"go.lumeweb.com/pinner-cli/internal/mcp"
)

// Options configures an embedded hosted Pinner MCP server.
type Options struct {
	// Surface declares which domains/tool families are exposed. A zero Surface
	// defaults to SurfaceHosted (the standard hosted set); use a partial
	// Surface to enable only specific families.
	Surface Surface

	// CatalogDeps supplies the operation-catalog dependency bundle for this
	// server: the Portal API endpoint and per-request credential resolution.
	// It is required — the compiler-backed catalog is the only source of the
	// tool surface. The Portal plugin constructs it (typically by wiring core
	// service factories against its API endpoint and a CredentialResolver).
	CatalogDeps func() *mcp.CatalogDepsBundle

	// CredentialResolver maps the OAuth-authenticated caller of a request onto
	// the Portal API token used to serve that request. It is threaded through
	// the operation dispatch so every hosted operation authenticates as the
	// calling user instead of a shared config token. When nil, ops fall back to
	// their config-token source.
	CredentialResolver CredentialResolver

	// ResourceFactory builds the pinner:// resource providers (account status,
	// websites platform domains, ...). The vault resource is omitted for a
	// hosted surface by construction. Optional.
	ResourceFactory mcp.ResourceProvidersFactory

	// ServerOptions enables optional custom-tool wiring (e.g. mcp.WithPrompts,
	// IPFS upload/download providers).
	ServerOptions []mcp.MCPServerOption

	// OAuthHandler protects the /mcp endpoint with OAuth. When nil, the
	// handler is served unauthenticated (the caller is responsible for any
	// upstream auth, e.g. Portal middleware).
	OAuthHandler OAuthHandler

	// DisableLocalhostProtection disables the Streamable-HTTP localhost
	// (DNS-rebinding) protection. Required when the handler is served behind a
	// proxy/tunnel that presents a non-loopback Origin.
	DisableLocalhostProtection bool
}

// New assembles an embedded hosted Pinner MCP server and returns its
// /mcp streamable-HTTP handler. It reuses the exact catalog, compiler,
// meta-tools, Apps/resource/prompt, and agent-guide machinery the CLI uses,
// restricted to the requested surface. The OAuthHandler (when provided) wraps
// the returned handler; Portal middleware or the handler then serve it.
//
// The caller wires hosting: serving the handler on a route, adding any Portal
// auth middleware, and serving the OAuth/.well-known endpoints via its own
// authorization server.
func New(opts Options) (http.Handler, error) {
	surface := opts.Surface
	if surface.IsZero() {
		surface = SurfaceHosted
	}
	srv, _, err := mcp.BuildHostedServer(mcp.HostedServerConfig{
		Surface: surface.toInternal(),
		// Seed the caller-supplied CredentialResolver onto the bundle so the
		// per-request token it resolves is threaded through operation dispatch.
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			if opts.CatalogDeps == nil {
				return nil
			}
			bundle := opts.CatalogDeps()
			if bundle != nil && opts.CredentialResolver != nil {
				bundle.CredentialResolver = opts.CredentialResolver
			}
			return bundle
		},
		ResourceFactory: opts.ResourceFactory,
		Options:         opts.ServerOptions,
	})
	if err != nil {
		return nil, err
	}

	handler := mcp.HTTPHandler(srv, opts.DisableLocalhostProtection)
	if opts.OAuthHandler != nil {
		handler = opts.OAuthHandler.WrapHTTP(handler)
	}
	return handler, nil
}
