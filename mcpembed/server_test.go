package mcpembed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSurfaceToInternalHosted verifies the public Surface maps onto the
// internal surface with the Sia vault and admin always disabled.
func TestSurfaceToInternalHosted(t *testing.T) {
	internal := SurfaceHosted.toInternal()
	assert.True(t, internal.AccountOn())
	assert.True(t, internal.PinsOn())
	assert.False(t, internal.VaultOn(), "hosted must never expose the Sia vault")
	assert.False(t, internal.AdminOn(), "hosted must never expose portal admin")

	// A partial hosted surface maps flag per-field (only Account set), with
	// vault always off.
	partial := Surface{Account: true}.toInternal()
	assert.True(t, partial.AccountOn())
	assert.False(t, partial.PinsOn(), "only Account was enabled, so Pins stays off")
	assert.False(t, partial.VaultOn(), "hosted must never expose the Sia vault")
}

// TestNewRequiresCatalogDeps verifies New fails fast when no operation-catalog
// dependency bundle is supplied, rather than returning a hollow server.
func TestNewRequiresCatalogDeps(t *testing.T) {
	_, err := New(Options{Surface: SurfaceHosted})
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
		Surface: SurfaceHosted,
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

var _ CredentialResolver = configCred{}

type configCred struct {
	token string
}

func (c configCred) TokenForRequest(ctx context.Context) (string, error) {
	if c.token == "" {
		return "", mcp.ErrNotAuthenticated
	}
	return c.token, nil
}
