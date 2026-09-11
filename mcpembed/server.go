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
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/cli"
	"go.lumeweb.com/pinner-cli/internal/mcp"
)

// bundleFrom invokes the CatalogDeps factory once, tolerating a nil factory.
func bundleFrom(factory func() *mcp.CatalogDepsBundle) *mcp.CatalogDepsBundle {
	if factory == nil {
		return nil
	}
	return factory()
}

// normalizeCredentialResolvers folds the pairwise normalizeCredentialResolver
// rule across the explicit Options resolver and EVERY sampled
// construction-time bundle resolver, so a non-uniform CatalogDeps factory
// (e.g. nil on the first invocation, a resolver afterwards) normalizes to the
// non-nil resolver for BOTH the HTTP middleware and catalog dispatch instead
// of leaving one boundary resolver-less, and two observed-but-disagreeing
// resolvers fail construction closed (the same conflict rule the pairwise
// check enforces).
func normalizeCredentialResolvers(explicit CredentialResolver, sampled []CredentialResolver) (CredentialResolver, error) {
	effective := explicit
	for _, r := range sampled {
		var err error
		effective, err = normalizeCredentialResolver(effective, r)
		if err != nil {
			return nil, err
		}
	}
	return effective, nil
}

// Options configures an embedded hosted Pinner MCP server.
type Options struct {
	// DomainScope declares which domains/tool families are exposed. A zero DomainScope
	// defaults to DomainScopeHosted (the standard hosted set); use a partial
	// DomainScope to enable only specific families.
	DomainScope DomainScope

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
// normalizeCredentialResolver resolves the ONE effective per-request
// credential resolver for this embed, so the operation-catalog dispatch and
// the HTTP/transfer path can never disagree about identity:
//
//   - nil Options.CredentialResolver defers to a resolver already supplied on
//     the CatalogDeps bundle (the "bundle-only" hosted setup): that resolver
//     then drives BOTH the bundle seeding and the credential middleware, so
//     direct transfers can never fall back to config credentials.
//   - both supplied and identical (pointer/value-equal) is accepted.
//   - both supplied and DIFFERENT is a construction wiring conflict — two
//     resolvers would let compiled operations and the transfer path
//     authenticate under different identities — and New fails closed.
//
// Uncomparable (closure-backed) resolver implementations cannot be proven
// value-identical by reflection, but that is NOT evidence of a conflict: a
// closure wired the same to both places is a valid (and common) hosted setup.
// Closures may therefore opt in to the proof with the
// IdentifiableCredentialResolver interface (a stable ResolverIdentity for the
// underlying credential source): equal identities are accepted as THE SAME
// resolver. Anything that proves neither value-equality nor a shared identity
// fails closed.
func normalizeCredentialResolver(explicit, fromBundle CredentialResolver) (CredentialResolver, error) {
	switch {
	case explicit == nil:
		return fromBundle, nil
	case fromBundle == nil:
		return explicit, nil
	}
	if sameCredentialResolver(explicit, fromBundle) {
		return explicit, nil
	}
	return nil, fmt.Errorf("mcpembed: conflicting credential resolvers: Options.CredentialResolver and the CatalogDeps bundle's resolver must be the same resolver (none provided here)")
}

// sameCredentialResolver reports whether a and b provably resolve through the
// SAME credential source: reflect value-equality when the concrete types are
// comparable, or — for uncomparable closure-backed resolvers — equal
// IdentifiableCredentialResolver identities. Inability to prove equality is a
// conflict (fail closed), never an accepted-equality shortcut.
func sameCredentialResolver(a, b CredentialResolver) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.Comparable() && bv.Comparable() {
		return av.Equal(bv)
	}
	ia, aok := a.(IdentifiableCredentialResolver)
	ib, bok := b.(IdentifiableCredentialResolver)
	if !aok || !bok {
		// Cannot prove the two closures are the same wired resolver; a
		// one-sided identity proves nothing.
		return false
	}
	iaID, ibID := ia.ResolverIdentity(), ib.ResolverIdentity()
	return iaID != "" && ibID != "" && iaID == ibID
}

// lateResolverBank is the ONE shared seat for a CatalogDeps resolver that is
// discovered in a LATER bundle-factory invocation — after construction, whose
// sampled factory calls (resolver extraction and transfer auto-wiring) all
// returned no resolver. Such a resolver is typically surfaced during
// assembly — populateCatalogTools / buildCatalog invoke the factory while
// materializing the catalog, not per request dispatch — and adoption there
// means the HTTP credential middleware and the catalog dispatch seeding use
// that same resolver and can never authenticate the same path under
// different identities. The first discovered resolver is
// pinned (adopt-first): a hosted CatalogDeps factory must supply the same
// resolver on every invocation anyway (the construction-time normalization
// enforces exactly that on the samples it observes), so a later invocation
// carrying a DIFFERENT resolver is forced onto the pinned one, keeping both
// boundaries deterministic. Concurrency-safe: dispatch factories may be
// invoked concurrently by simultaneous /mcp requests.
type lateResolverBank struct {
	mu   sync.Mutex
	some CredentialResolver
}

// adopt records r as the late-discovered resolver. The first resolver wins and
// is returned; subsequent adopters receive the pinned one (and their argument
// is ignored).
func (b *lateResolverBank) adopt(r CredentialResolver) CredentialResolver {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.some == nil {
		b.some = r
	}
	return b.some
}

// get returns the pinned late-discovered resolver, or nil when none has been
// discovered yet.
func (b *lateResolverBank) get() CredentialResolver {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.some
}

// lateDiscoveryResolver is the CredentialResolver installed on the HTTP
// credential middleware while no construction-time resolver is in force. It
// forwards to the late-discovered resolver once one has been adopted (fail
// closed on its errors/blank tokens, like any configured resolver), and
// reports ErrCredentialsNotConfigured before discovery so the middleware
// behaves exactly like an absent middleware (config-token fallback) instead of
// 401-ing a hosted embed that has no credentials configured at all.
type lateDiscoveryResolver struct{ bank *lateResolverBank }

func (l lateDiscoveryResolver) TokenForRequest(ctx context.Context) (string, error) {
	r := l.bank.get()
	if r == nil {
		return "", mcp.ErrCredentialsNotConfigured
	}
	return r.TokenForRequest(ctx)
}

func New(opts Options) (http.Handler, error) {
	surface := opts.DomainScope
	if surface.IsZero() {
		surface = DomainScopeHosted
	}

	// Resolve the ONE effective credential resolver at construction time —
	// never separately per path — so the compiled catalog and every
	// HTTP/transfer path authenticate under the same resolver.
	//
	// EVERY construction-time bundle-factory invocation is sampled into the
	// normalization: New invokes the factory once for resolver extraction and
	// once for transfer auto-wiring, and both observed resolvers fold into the
	// same pairwise rule below. A non-uniform factory (nil bundle first,
	// resolver-bearing bundle afterwards) therefore normalizes to the non-nil
	// resolver for BOTH the HTTP middleware and the catalog dispatch seeding —
	// sampling only the first invocation would build the middleware
	// resolver-less while catalog dispatch seeds the later resolver's
	// credentials, i.e. divergent identities across the two boundaries. Two
	// observed-but-disagreeing resolvers still fail construction closed.
	resolverBundle := bundleFrom(opts.CatalogDeps)
	wireBundle := bundleFrom(opts.CatalogDeps)
	resolverSamples := make([]CredentialResolver, 0, 2)
	for _, bundle := range []*mcp.CatalogDepsBundle{resolverBundle, wireBundle} {
		if bundle != nil {
			resolverSamples = append(resolverSamples, bundle.CredentialResolver)
		}
	}
	effectiveResolver, err := normalizeCredentialResolvers(opts.CredentialResolver, resolverSamples)
	if err != nil {
		return nil, err
	}

	// The credential seat shared by the HTTP middleware and dispatch. When a
	// construction-time resolver is in force it drives both directly; when
	// none was configured at construction, the middleware installs a
	// discovery adapter that adopts the first late-discovered resolver (and
	// passes through like an absent middleware until then), so a factory
	// whose resolver appears only in a later assembly/dispatch factory
	// invocation still yields ONE identity across both boundaries instead of
	// dispatch-only adoption.
	lateDiscovery := &lateResolverBank{}
	httpResolver := effectiveResolver
	if httpResolver == nil {
		httpResolver = lateDiscoveryResolver{bank: lateDiscovery}
	}

	// Auto-wire the IPFS upload/download transfer surface when the sampled
	// construction-time bundle carries a live config manager (the wireBundle
	// sample above — no extra factory invocation). A hosted embed never wires
	// vault, so these are the only transfer executors. Caller-supplied
	// ServerOptions are appended after these, so an explicit override always
	// wins.
	var transferOpts []mcp.MCPServerOption
	if wireBundle != nil && wireBundle.CfgMgr != nil {
		if cfgMgr, err := resolveCfgMgr(wireBundle); err == nil && cfgMgr != nil {
			if auto, aerr := cli.BuildHostedTransferOptions(cfgMgr); aerr == nil {
				transferOpts = auto
			}
		}
	}
	serverOptions := append(append([]mcp.MCPServerOption{}, transferOpts...), opts.ServerOptions...)

	srv, _, ht, err := mcp.BuildHostedServer(mcp.HostedServerConfig{
		DomainScope: surface.toInternal(),
		// Seed the one effective CredentialResolver onto the bundle so the
		// per-request token it resolves is threaded through operation dispatch,
		// and resolve LATE DISCOVERY per invocation: when the construction-time
		// effective resolver is nil (every sampled factory invocation returned
		// no resolver) a runtime bundle that suddenly carries its own resolver
		// is adopted into the shared bank — installed on the HTTP credential
		// middleware below via the lateDiscoveryResolver — so dispatch and the
		// HTTP boundary authenticate as the SAME adopted identity instead of
		// dispatch alone picking up the late resolver while the byte routes
		// stay resolver-less. The first adopted resolver is pinned; later
		// invocation-specific resolvers are forced onto it (a hosted factory
		// must be resolver-stable per the construction-time contract, so
		// accepting a different one here would reintroduce divergent
		// identities).
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			if opts.CatalogDeps == nil {
				return nil
			}
			bundle := opts.CatalogDeps()
			if bundle == nil {
				return nil
			}
			effective := effectiveResolver
			if effective == nil {
				if bundle.CredentialResolver != nil {
					effective = lateDiscovery.adopt(bundle.CredentialResolver)
				} else {
					effective = lateDiscovery.get()
				}
			}
			if effective != nil {
				bundle.CredentialResolver = effective
			}
			return bundle
		},
		ResourceFactory: opts.ResourceFactory,
		Options:         serverOptions,
		BaseURL:         opts.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	// Install the credential middleware so the per-request Portal API JWT is
	// resolved once at the HTTP boundary and carried on the context to every
	// handler (catalog + custom tools, including the direct transfer tools).
	// httpResolver is EITHER the one normalized construction-time effective
	// resolver (which fails closed on an error/blank token, so no direct
	// transfer can fall back to config credentials) OR — when no resolver was
	// configured at construction — the lateDiscoveryResolver, which behaves
	// like an absent middleware (config-token fallback) until a CatalogDeps
	// factory invocation surfaces a resolver, and then forwards to it with the
	// same fail-closed semantics, resolving the adopted identity for BOTH the
	// HTTP boundary and dispatch.
	streamable := mcp.HTTPHandler(srv, httpResolver, opts.DisableLocalhostProtection)
	if opts.OAuthHandler != nil {
		streamable = opts.OAuthHandler.WrapHTTP(streamable)
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
