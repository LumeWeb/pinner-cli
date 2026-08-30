package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCredResolver is a minimal CredentialResolver returning a fixed token.
type testCredResolver struct{ tok string }

func (r testCredResolver) TokenForRequest(_ context.Context) (string, error) { return r.tok, nil }

// TestCredentialContextRoundTrip verifies WithCredential/CredentialFromContext
// store and return the JWT, and that an unset context yields "".
func TestCredentialContextRoundTrip(t *testing.T) {
	assert.Equal(t, "", CredentialFromContext(context.Background()), "unset context must yield empty credential")

	ctx := WithCredential(context.Background(), "portal-jwt-1")
	assert.Equal(t, "portal-jwt-1", CredentialFromContext(ctx), "stored credential must round-trip")
}

// TestCredentialMiddlewareStoresResolvedToken verifies the middleware resolves
// the Portal API JWT once per request and carries it on the request context, so
// every downstream handler reads a single consistent identity.
func TestCredentialMiddlewareStoresResolvedToken(t *testing.T) {
	resolver := testCredResolver{tok: "portal-jwt-7"}
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = CredentialFromContext(r.Context())
	})
	handler := credentialMiddleware(resolver, next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "portal-jwt-7", got, "middleware must store the resolved token on the request context")
}

// TestCredentialMiddlewareNilResolverPassThrough verifies that without a
// resolver (CLI/local path) the middleware does not store a credential and the
// request passes through unchanged.
func TestCredentialMiddlewareNilResolverPassThrough(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = CredentialFromContext(r.Context())
	})
	handler := credentialMiddleware(nil, next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "", got, "nil resolver must not inject a credential")
}

// TestCompiledHandlerPrefersContextCredential verifies compiledHandler uses the
// credential already established on the context (by the HTTP middleware) and
// does NOT re-call the resolver, so identity is resolved once per request.
func TestCompiledHandlerPrefersContextCredential(t *testing.T) {
	const ctxJWT = "from-http-middleware"
	cat := &captureCatalog{}
	resolveCalled := false
	resolveToken := func(ctx context.Context) (string, error) {
		resolveCalled = true
		return "from-resolver", nil
	}

	h := compiledHandler(cat, "pins_list", resolveToken)
	_, err := h(WithCredential(context.Background(), ctxJWT), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, resolveCalled, "resolver must not be re-called when a credential is already on the context")
	assert.Equal(t, ctxJWT, cat.input[catalog.ReservedAuthTokenKey], "context credential must be injected into op input")
}

// TestCompiledHandlerFallbackResolvesAndInjects verifies the stdio path (no
// context credential, no middleware) falls back to resolveToken and still
// threads the resolved token into the op input.
func TestCompiledHandlerFallbackResolvesAndInjects(t *testing.T) {
	const jwt = "from-resolver"
	cat := &captureCatalog{}
	resolveToken := func(ctx context.Context) (string, error) { return jwt, nil }

	h := compiledHandler(cat, "pins_list", resolveToken)
	_, err := h(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, jwt, cat.input[catalog.ReservedAuthTokenKey], "resolver token must be injected when context is empty")
}
