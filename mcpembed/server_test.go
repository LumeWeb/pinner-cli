package mcpembed

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.lumeweb.com/pinner/core/config"

	"go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDomainScopeToInternalHosted verifies the public DomainScope maps onto the
// internal surface with the Sia vault and admin always disabled.
func TestDomainScopeToInternalHosted(t *testing.T) {
	internal := DomainScopeHosted.toInternal()
	assert.True(t, internal.AccountOn())
	assert.True(t, internal.PinsOn())
	assert.False(t, internal.VaultOn(), "hosted must never expose the Sia vault")
	assert.False(t, internal.AdminOn(), "hosted must never expose portal admin")

	// A partial hosted surface maps flag per-field (only Account set), with
	// vault always off.
	partial := DomainScope{Account: true}.toInternal()
	assert.True(t, partial.AccountOn())
	assert.False(t, partial.PinsOn(), "only Account was enabled, so Pins stays off")
	assert.False(t, partial.VaultOn(), "hosted must never expose the Sia vault")
}

// TestNewRequiresCatalogDeps verifies New fails fast when no operation-catalog
// dependency bundle is supplied, rather than returning a hollow server.
func TestNewRequiresCatalogDeps(t *testing.T) {
	_, err := New(Options{DomainScope: DomainScopeHosted})
	require.Error(t, err, "New without CatalogDeps must fail")
}

// TestNewOAuthHandlerApplied verifies the OAuthHandler wraps the produced
// handler and that the handler is a working http.Handler.
func TestNewOAuthHandlerApplied(t *testing.T) {
	// Minimal, non-nil deps bundle so the hosted server assembles. Real
	// service factories are supplied by the Portal plugin in production; a
	// bare deps struct yields ops that degrade to "service unavailable" at
	// invoke time, which is fine for handler-assembly coverage.
	deps := &mcp.CatalogDepsBundle{}

	wrapped := false
	handler, err := New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return deps
		},
		OAuthHandler: oauthStub{wrap: func(next http.Handler) http.Handler {
			wrapped = true
			return next
		}},
	})
	require.NoError(t, err, "New must assemble a handler")
	require.NotNil(t, handler)
	assert.True(t, wrapped, "OAuthHandler.WrapHTTP must be applied")

	// The produced handler must respond to a (likely rejected/initialization)
	// request without panicking.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

type oauthStub struct {
	wrap func(http.Handler) http.Handler
}

func (o oauthStub) WrapHTTP(next http.Handler) http.Handler {
	if o.wrap != nil {
		return o.wrap(next)
	}
	return next
}

// TestNewWiresIPFSTransferByteRoutes verifies that a hosted embed whose catalog
// deps carry a live config manager auto-wires the IPFS transfer surface, and
// that the returned handler serves both the /mcp streamable endpoint and the
// IPFS presigned upload/drop byte-route paths. Never vault.
func TestNewWiresIPFSTransferByteRoutes(t *testing.T) {
	cfgMgr := newTestCfgMgr(t)
	deps := &mcp.CatalogDepsBundle{CfgMgr: func() config.Manager { return cfgMgr }}

	handler, err := New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle { return deps },
	})
	require.NoError(t, err, "New with a functioning CatalogDeps must assemble")
	require.NotNil(t, handler)

	// The handler should serve the /mcp streamable endpoint (a request is at
	// least guaranteed to be handled or rejected without panicking).
	mcpRec := httptest.NewRecorder()
	handler.ServeHTTP(mcpRec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)))
	require.NotEqual(t, http.StatusNotFound, mcpRec.Code, "/mcp must be routed to the streamable handler")

	// Byte routes: /upload/<token> and /download/<token> must be MOUNTED on the
	// returned mux. An unminted token is legitimately rejected by the
	// coordinator with its own 404 — the distinguishing signal vs a router-level
	// 404 is the coordinator's body text ("upload endpoint"), which proves the
	// route reached the coordinator rather than falling through the mux.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/upload/0102030405", strings.NewReader("")))
	require.Contains(t, rec.Body.String(), "upload endpoint", "PUT /upload/... must be handled by the presigned coordinator")

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download/0102030405", strings.NewReader("")))
	require.Contains(t, rec.Body.String(), "download", "GET /download/... must be handled by the filedrop coordinator")
}

// stubTokenResolver is the ONE token-resolver fixture for the hosted embed
// tests: a CredentialResolver that resolves one fixed token, blank →
// mcp.ErrNotAuthenticated (the production shape of a Portal IdP backend that
// CAN resolve the caller). Its type is comparable so tests can pin identity
// semantics — both resolver identity and blank-token behavior for every
// consumer in this file.
var _ CredentialResolver = stubTokenResolver{}

type stubTokenResolver struct{ token string }

func (r stubTokenResolver) TokenForRequest(ctx context.Context) (string, error) {
	if r.token == "" {
		return "", mcp.ErrNotAuthenticated
	}
	return r.token, nil
}

// callToolDrive sends a raw tools/call over the assembled /mcp endpoint (the
// stateless streamable path), like the internal/mcp credential HTTP regression.
func callToolDrive(t *testing.T, handler http.Handler, tool, arguments string) (int, string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + arguments + `}}`
	req, err := http.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	req.Header.Set("Mcp-Method", "tools/call")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestBundleOnlyResolverReachesTransferContext is the hosted HTTP/transfer
// security regression: a hosted embed whose ONLY credential resolver lives on
// the CatalogDeps bundle (Options.CredentialResolver nil) must still install
// the fail-closed credential middleware with THAT resolver, so a direct
// transfer tool call (download_file) authenticates as the resolved caller —
// the bundle-resolved token reaches the transfer executor's context — and a
// resolver error or blank token rejects the request (HTTP 401) before any
// transfer executor runs. No config-credential fallback happens on any path.
func TestBundleOnlyResolverReachesTransferContext(t *testing.T) {
	const callerToken = "caller-jwt-42"

	var executed int
	var gotCred string
	newHandler := func(resolver CredentialResolver) http.Handler {
		executed = 0
		gotCred = ""
		handler, err := New(Options{
			DomainScope: DomainScopeHosted,
			CatalogDeps: func() *mcp.CatalogDepsBundle {
				return &mcp.CatalogDepsBundle{
					CfgMgr:             func() config.Manager { return newTestCfgMgr(t) },
					CredentialResolver: resolver,
				}
			},
			ServerOptions: []mcp.MCPServerOption{
				// Override the auto-wired real executor with a stub that
				// captures the per-request credential it was handed.
				mcp.WithDownloadRoot(func() string { return t.TempDir() }),
				mcp.WithIPFSDownload(transfer.IPFSDownloadHandler(func(ctx context.Context, path string, w io.Writer) error {
					executed++
					gotCred = mcp.CredentialFromContext(ctx)
					return nil
				})),
			},
		})
		require.NoError(t, err, "New with a bundle-only resolver must assemble")
		return handler
	}

	// Positive control: the bundle-only resolver resolves the caller, the
	// middleware injects the credential, and the DIRECT transfer executor
	// receives it on its context.
	handler := newHandler(stubTokenResolver{token: callerToken})
	code, body := callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	require.Equal(t, http.StatusOK, code, "tools/call download_file failed: %s", body)
	require.Equal(t, 1, executed, "the transfer executor must run for the authenticated caller")
	require.Equal(t, callerToken, gotCred,
		"the bundle-only resolver's token must reach the transfer executor context — never a config fallback")

	// Fail closed 1: the bundle-only resolver ERRORS — the request is rejected
	// at the HTTP boundary and the transfer executor never runs.
	handler = newHandler(erringResolver{})
	code, _ = callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	require.Equal(t, http.StatusUnauthorized, code,
		"a configured bundle-only resolver that errors must reject the request (401), not fall back to config credentials")
	require.Zero(t, executed, "no transfer may execute without a resolved identity")

	// Fail closed 2: the bundle-only resolver returns a BLANK token.
	handler = newHandler(stubTokenResolver{token: ""})
	code, _ = callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	require.Equal(t, http.StatusUnauthorized, code, "a blank resolved token must fail closed")
	require.Zero(t, executed, "no transfer may execute under a blank identity")
}

// erringResolver always fails (Portal IdP unavailable).
type erringResolver struct{}

func (erringResolver) TokenForRequest(context.Context) (string, error) {
	return "", mcp.ErrNotAuthenticated
}

// TestNewNormalizesCredentialResolver pins the one-resolver normalization:
//   - a resolver supplied BOTH on Options and on the bundle must be THE SAME
//     resolver (identical comparable value) or New rejects the conflict;
//   - Options.CredentialResolver alone applies (seeding the bundle).
func TestNewNormalizesCredentialResolver(t *testing.T) {
	caught := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "conflicting credential resolvers")
	}

	// Same resolver both places: accepted.
	same := stubTokenResolver{token: "jwt"}
	handler, err := New(Options{
		DomainScope:        DomainScopeHosted,
		CatalogDeps:        func() *mcp.CatalogDepsBundle { return &mcp.CatalogDepsBundle{CredentialResolver: same} },
		CredentialResolver: same,
	})
	require.NoError(t, err)
	require.NotNil(t, handler)

	// Two DIFFERENT resolvers: construction fails closed.
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return &mcp.CatalogDepsBundle{CredentialResolver: stubTokenResolver{token: "other"}}
		},
		CredentialResolver: same,
	})
	require.True(t, caught(err), "two different supplied resolvers must reject construction, got: %v", err)
	require.Nil(t, handler)

	// Bundle-only and Options-only still assemble (normalization picks the one
	// effective resolver; covered end-to-end by the transfer-context test and
	// the existing OAuth/HTTP suites).
	handler, err = New(Options{
		DomainScope:        DomainScopeHosted,
		CatalogDeps:        func() *mcp.CatalogDepsBundle { return &mcp.CatalogDepsBundle{} },
		CredentialResolver: same,
	})
	require.NoError(t, err)
	require.NotNil(t, handler)
}

// TestNewNormalizeResolverAcrossFactoryInvocations pins finding: the ONE
// effective credential resolver is normalized across EVERY construction-time
// CatalogDeps factory invocation, not just the first one. A non-uniform
// factory whose FIRST invocation yields a nil bundle (no resolver) and whose
// later invocations carry a resolver would otherwise leave the HTTP
// credential middleware resolver-less while catalog dispatch seeds the later
// resolver's per-request credentials — divergent identities across the two
// boundaries. Construction instead adopts the non-nil resolver for both, the
// middleware fails closed on it, and the transfer executor receives the
// adopted token. Two observed-but-DISAGREEING resolvers fail construction.
func TestNewNormalizeResolverAcrossFactoryInvocations(t *testing.T) {
	const callerToken = "late-bundle-jwt"

	calls := 0
	nonUniformFactory := func() *mcp.CatalogDepsBundle {
		calls++
		// First construction-time sampling call: nil bundle, no resolver.
		if calls == 1 {
			return nil
		}
		return &mcp.CatalogDepsBundle{
			CfgMgr:             func() config.Manager { return newTestCfgMgr(t) },
			CredentialResolver: stubTokenResolver{token: callerToken},
		}
	}

	handler, err := New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: nonUniformFactory,
		ServerOptions: []mcp.MCPServerOption{
			mcp.WithDownloadRoot(func() string { return t.TempDir() }),
			mcp.WithIPFSDownload(transfer.IPFSDownloadHandler(func(ctx context.Context, path string, w io.Writer) error {
				return nil
			})),
		},
	})
	// Adoption: the first nil sample normalizes against the later non-nil
	// resolver, so construction succeeds with the LATER resolver in force.
	require.NoError(t, err, "a non-uniform factory (nil first, resolver after) adopts the non-nil resolver")
	require.NotNil(t, handler)
	require.GreaterOrEqual(t, calls, 2, "both construction-time boundaries sample the factory")

	// The adopted resolver drives the HTTP credential middleware: a transfer
	// call authenticates with the adopted token — exactly one boundary
	// identity, never a resolver-less middleware.
	var executed int
	var gotCred string
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: nonUniformFactory,
		ServerOptions: []mcp.MCPServerOption{
			mcp.WithDownloadRoot(func() string { return t.TempDir() }),
			mcp.WithIPFSDownload(transfer.IPFSDownloadHandler(func(ctx context.Context, path string, w io.Writer) error {
				executed++
				gotCred = mcp.CredentialFromContext(ctx)
				return nil
			})),
		},
	})
	require.NoError(t, err)

	code, body := callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	require.Equal(t, http.StatusOK, code, "tools/call download_file failed: %s", body)
	require.Equal(t, 1, executed, "the transfer executor must run for the adopted resolver's caller")
	require.Equal(t, callerToken, gotCred,
		"the adopted (later-sample) resolver's token must reach the transfer executor context for BOTH boundaries")

	// Two observed-but-differing resolvers across construction-time
	// invocations: construction fails closed.
	var call2 int
	conflicting := func() *mcp.CatalogDepsBundle {
		call2++
		r := stubTokenResolver{token: "first-jwt"}
		if call2 > 1 {
			r = stubTokenResolver{token: "second-jwt"}
		}
		return &mcp.CatalogDepsBundle{CredentialResolver: r}
	}
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: conflicting,
	})
	require.True(t, err != nil && strings.Contains(err.Error(), "conflicting credential resolvers"),
		"disagreeing construction-time resolvers must fail construction, got: %v", err)
	require.Nil(t, handler)
}

// identifiedFunc is an UNCOMPARABLE (func-typed) CredentialResolver that
// implements IdentifiableCredentialResolver: it models the production shape of
// an embedding host's closure resolver (a func closing over request state)
// that also names its underlying credential source. This variant SHARES one
// identity across all closures built from it.
type identifiedFunc func(ctx context.Context) (string, error)

func (f identifiedFunc) TokenForRequest(ctx context.Context) (string, error) {
	return f(ctx)
}

func (f identifiedFunc) ResolverIdentity() string {
	return "hosted-user-mapping-v1"
}

// otherIdentifiedFunc is another identity-bearing closure type with a
// DIFFERENT stable identity — the "two closures wrapping different credential
// sources" case that must still fail construction closed.
type otherIdentifiedFunc func(ctx context.Context) (string, error)

func (f otherIdentifiedFunc) TokenForRequest(ctx context.Context) (string, error) {
	return f(ctx)
}

func (f otherIdentifiedFunc) ResolverIdentity() string {
	return "hosted-workspace-mapping-v1"
}

func TestNewAcceptsIdenticalSharedClosureResolver(t *testing.T) {
	// The SAME closure wired as BOTH Options.CredentialResolver and the
	// CatalogDeps bundle's resolver: reflect cannot compare func values, but
	// the resolver's stable ResolverIdentity proves it is one resolver —
	// construction must succeed, not reject a valid shared wiring.
	shared := identifiedFunc(func(ctx context.Context) (string, error) {
		return "closure-jwt", nil
	})
	handler, err := New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return &mcp.CatalogDepsBundle{CredentialResolver: shared}
		},
		CredentialResolver: shared,
	})
	require.NoError(t, err, "the SAME closure wired to both Options and the bundle must construct (identity proof), got: %v", err)
	require.NotNil(t, handler)

	// Two DIFFERENT closures that name the SAME underlying credential source
	// (equal identities) are also the same resolver for wiring purposes.
	b := identifiedFunc(func(ctx context.Context) (string, error) {
		return "closure-jwt", nil
	})
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return &mcp.CatalogDepsBundle{CredentialResolver: b}
		},
		CredentialResolver: shared,
	})
	require.NoError(t, err, "two closures with a shared identity are the same resolver")
	require.NotNil(t, handler)

	// Resolvers with DIFFERENT identities still fail closed: differing
	// identities are differing credential sources.
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return &mcp.CatalogDepsBundle{CredentialResolver: otherIdentifiedFunc(func(ctx context.Context) (string, error) {
				return "other-jwt", nil
			})}
		},
		CredentialResolver: shared,
	})
	require.Error(t, err, "different identities must still be a construction conflict, got: %v", err)
	require.Nil(t, handler)

	// Uncomparable values with NO identity mechanism at all: still fail
	// closed — inability to prove equality is a conflict, never an accept.
	handler, err = New(Options{
		DomainScope: DomainScopeHosted,
		CatalogDeps: func() *mcp.CatalogDepsBundle {
			return &mcp.CatalogDepsBundle{CredentialResolver: unidentifiedFuncStub{}}
		},
		CredentialResolver: anotherUnidentifiedStub{},
	})
	require.Error(t, err, "two uncomparable, unidentified resolvers must remain a conflict (fail closed)")
	require.Nil(t, handler)
}

// anotherUnidentifiedStub and unidentifiedFuncStub are uncomparable resolver
// values (func/slice fields) that carry NO ResolverIdentity — the
// cannot-prove-equality case that must keep failing construction closed.
type anotherUnidentifiedStub struct {
	fn func(ctx context.Context) (string, error)
}

func (anotherUnidentifiedStub) TokenForRequest(context.Context) (string, error) {
	return "", mcp.ErrNotAuthenticated
}

type unidentifiedFuncStub struct {
	buf []byte
}

func (unidentifiedFuncStub) TokenForRequest(context.Context) (string, error) {
	return "", mcp.ErrNotAuthenticated
}

// TestNewLateResolverDiscoveryAdoptedByBothBoundaries pins finding: a
// CatalogDeps factory whose resolver appears only from invocation three onward
// (all construction-time samples resolver-less) is adopted for BOTH
// boundaries — catalog/dispatch seeding AND the HTTP credential middleware —
// so the same path can never authenticate under different identities.
func TestNewLateResolverDiscoveryAdoptedByBothBoundaries(t *testing.T) {
	const lateToken = "late-discovery-jwt"

	var executed int
	var gotCred string

	newHandler := func(token string) http.Handler {
		calls := 0
		handler, err := New(Options{
			DomainScope: DomainScopeHosted,
			CatalogDeps: func() *mcp.CatalogDepsBundle {
				calls++
				if calls <= 2 {
					// Construction-time sampling: bundle, but NO resolver.
					return &mcp.CatalogDepsBundle{CfgMgr: func() config.Manager { return newTestCfgMgr(t) }}
				}
				// Discovered only from invocation three onward.
				return &mcp.CatalogDepsBundle{
					CfgMgr:             func() config.Manager { return newTestCfgMgr(t) },
					CredentialResolver: stubTokenResolver{token: token},
				}
			},
			ServerOptions: []mcp.MCPServerOption{
				mcp.WithDownloadRoot(func() string { return t.TempDir() }),
				mcp.WithIPFSDownload(transfer.IPFSDownloadHandler(func(ctx context.Context, path string, w io.Writer) error {
					executed++
					gotCred = mcp.CredentialFromContext(ctx)
					return nil
				})),
			},
		})
		require.NoError(t, err, "a late-discovered resolver must still assemble a hosted handler")
		require.NotNil(t, handler)
		require.GreaterOrEqual(t, calls, 2, "construction samples the factory before discovery")
		return handler
	}

	// Positive: the late resolver is adopted by dispatch seeding — the
	// transfer executor authenticates with the LATE resolver's token on both
	// boundaries (the HTTP middleware forwards to the same adopted resolver).
	executed = 0
	gotCred = ""
	handler := newHandler(lateToken)
	code, body := callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	require.Equal(t, http.StatusOK, code, "tools/call download_file failed: %s", body)
	require.Equal(t, 1, executed, "the transfer executor must run under the adopted late resolver")
	require.Equal(t, lateToken, gotCred, "the adopted late resolver's token must reach the executor context")

	// Fail-closed consistency: after adoption, the HTTP credential middleware
	// forwards to the SAME adopted resolver — a blank/erroring late resolver
	// is rejected at the HTTP boundary with the middleware's 401 (never a
	// resolver-less middleware falling back to config credentials).
	executed = 0
	gotCred = ""
	handler = newHandler("")
	code, body = callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	if code == http.StatusOK {
		// First call may pass through (pre-/just-after adoption order);
		// whatever happens, a SECOND call must hit the middleware that now
		// resolves the adopted blank token.
		code, body = callToolDrive(t, handler, "download_file", `{"ipfs_path":"bafystub","sink":"local"}`)
	}
	require.Equal(t, http.StatusUnauthorized, code,
		"after late adoption the HTTP middleware resolves the SAME adopted resolver and must fail closed (401); got: %s", body)
	require.Contains(t, body, "no authenticated user for this request",
		"the 401 must come from the HTTP credential middleware (the same adopted identity source), not dispatch-only handling")
	require.Zero(t, executed, "no transfer may execute under a blank/errored adopted identity")
}

// newTestCfgMgr builds a throwaway config.Manager for hosted transfer tests. It
// needs a real base endpoint so the IPFS upload/download executors can be
// constructed (the config manager otherwise has no API endpoint to resolve).
func newTestCfgMgr(t *testing.T) config.Manager {
	t.Helper()
	dir := t.TempDir()
	cfgMgr, err := config.NewManager(dir + "/config.yaml")
	require.NoError(t, err)
	require.NoError(t, cfgMgr.SetBaseEndpoint("https://pinner.xyz"))
	require.NoError(t, cfgMgr.SetSecure(true))
	return cfgMgr
}
