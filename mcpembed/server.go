package mcpembed

// This package transitively embeds the templ output, MCP App JS bundles, and
// Tailwind stylesheet (via internal/mcp → internal/mcpapp), so `go generate
// ./mcpembed` regenerates them before any `go build`/`go test`. go:generate
// runs with this package as its working directory; `..` is the repo root where
// the Makefile lives. The `mcpembed` target installs templ, regenerates the
// templ files, and runs `pnpm install --frozen-lockfile` plus the app builds
// and CSS compile.
//go:generate make -C .. mcpembed

import (
	"net/http"

	"go.lumeweb.com/pinner-cli/internal/cli"
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
	// IPFS upload/download providers). IPFS upload/download providers are wired
	// automatically from CatalogDeps unless overridden here.
	ServerOptions []mcp.MCPServerOption

	// OAuthHandler protects the /mcp endpoint with OAuth. When nil, the
	// handler is served unauthenticated (the caller is responsible for any
	// upstream auth, e.g. Portal middleware).
	OAuthHandler OAuthHandler

	// DisableLocalhostProtection disables the Streamable-HTTP localhost
	// (DNS-rebinding) protection. Required when the handler is served behind a
	// proxy/tunnel that presents a non-loopback Origin.
	DisableLocalhostProtection bool

	// BaseURL is the externally reachable origin of this hosted server (e.g.
	// https://pinner.xyz). It is used to mint reachable presigned upload PUT and
	// filedrop GET URLs for the IPFS byte-route coordinators. When empty, the
	// coordinators fall back to their loopback-derived origin (correct only for
	// a host that serves the handler locally or applies its own base via
	// ServerOptions).
	BaseURL string
}

// New assembles an embedded hosted Pinner MCP server and returns its
// /mcp streamable-HTTP handler. It reuses the exact catalog, compiler,
// meta-tools, Apps/resource/prompt, and agent-guide machinery the CLI uses,
// restricted to the requested surface. The OAuthHandler (when provided) wraps
// the /mcp endpoint; Portal middleware or the handler then serve it.
//
// When CatalogDeps supplies a config manager, New automatically wires the IPFS
// upload/download transfer surface (upload_file, download_file, host_file_input)
// resolved against that config manager at request time — never the Sia vault.
// The returned handler then serves the MCP streamable endpoint on /mcp plus the
// IPFS presigned PUT (/upload/) and filedrop GET (/download/) byte routes, so an
// embedding host that routes those paths gets the full transfer surface.
//
// The caller wires hosting: serving the handler on a route, adding any Portal
// auth middleware, and serving the OAuth/.well-known endpoints via its own
// authorization server.
func New(opts Options) (http.Handler, error) {
	surface := opts.Surface
	if surface.IsZero() {
		surface = SurfaceHosted
	}

	// Auto-wire the IPFS upload/download transfer surface when the catalog deps
	// carry a live config manager. A hosted embed never wires vault, so these are
	// the only transfer executors. Caller-supplied ServerOptions are appended
	// after these, so an explicit override always wins.
	var transferOpts []mcp.MCPServerOption
	if opts.CatalogDeps != nil {
		if bundle := opts.CatalogDeps(); bundle != nil && bundle.CfgMgr != nil {
			if cfgMgr, err := resolveCfgMgr(bundle); err == nil && cfgMgr != nil {
				if auto, aerr := cli.BuildHostedTransferOptions(cfgMgr); aerr == nil {
					transferOpts = auto
				}
			}
		}
	}
	serverOptions := append(append([]mcp.MCPServerOption{}, transferOpts...), opts.ServerOptions...)

	srv, _, ht, err := mcp.BuildHostedServer(mcp.HostedServerConfig{
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
		Options:         serverOptions,
	})
	if err != nil {
		return nil, err
	}

	// Install the credential middleware when a resolver is present so the
	// per-request Portal API JWT is resolved once at the HTTP boundary and
	// carried on the context to every handler (catalog + custom tools).
	streamable := mcp.HTTPHandler(srv, opts.CredentialResolver, opts.DisableLocalhostProtection)
	if opts.OAuthHandler != nil {
		streamable = opts.OAuthHandler.WrapHTTP(streamable)
	}

	// Point the byte-route coordinators at the public origin (when known) so the
	// presigned PUT/GET URLs a hosted agent mints are actually reachable.
	if opts.BaseURL != "" {
		if ht != nil {
			if ht.Upload != nil {
				ht.Upload.SetBaseURL(opts.BaseURL)
			}
			if ht.Download != nil {
				ht.Download.SetBaseURL(opts.BaseURL)
			}
		}
	}

	// Mount the streamable MCP endpoint plus the IPFS byte routes on a mux, so
	// an embedding host that routes /upload and /download reaches the coordinators
	// out of band (the same byte flows the CLI/tunnel path serves on its mux).
	if ht == nil || (ht.Upload == nil && ht.Download == nil) {
		return streamable, nil
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", streamable)
	if ht.Upload != nil {
		ht.Upload.RegisterHandlers(mux)
	}
	if ht.Download != nil {
		ht.Download.RegisterHandlers(mux)
	}
	return mux, nil
}
