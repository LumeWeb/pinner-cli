package mcp

import (
	"context"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureCatalog is a minimal catalog.Catalog that records the input map each
// op is invoked with, so a test can observe what compiledHandler threaded in.
type captureCatalog struct {
	input map[string]any
}

func (c *captureCatalog) Add(op catalog.Operation) error                                   { return nil }
func (c *captureCatalog) Get(name string) (catalog.Operation, bool)                       { return nil, false }
func (c *captureCatalog) Search(query, category string, v catalog.Visibility) []catalog.Operation {
	return nil
}
func (c *captureCatalog) Describe(name string, actor catalog.Actor) (catalog.ToolDescriptor, bool) {
	return catalog.ToolDescriptor{}, false
}
func (c *captureCatalog) Invoke(ctx context.Context, name string, input map[string]any, actor catalog.Actor) (any, error) {
	c.input = input
	return "ok", nil
}

var _ catalog.Catalog = (*captureCatalog)(nil)

// TestCompiledHandlerInjectsResolvedToken verifies that when a hosted server
// supplies a CredentialResolver, compiledHandler resolves the per-request token
// and threads it through the reserved auth-token input override so the op
// authenticates as the calling user, not a shared config credential.
func TestCompiledHandlerInjectsResolvedToken(t *testing.T) {
	const jwt = "portal-jwt-for-user-42"
	cat := &captureCatalog{}
	resolveToken := func(ctx context.Context) (string, error) { return jwt, nil }

	h := compiledHandler(cat, "pins_list", resolveToken)
	_, err := h(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, jwt, cat.input[catalog.ReservedAuthTokenKey], "resolved token must be injected into op input")
}

// TestCompiledHandlerNoInjectionWithoutResolver verifies the CLI/local path
// (nil resolver) does not inject a token, so ops fall back to config.
func TestCompiledHandlerNoInjectionWithoutResolver(t *testing.T) {
	cat := &captureCatalog{}
	h := compiledHandler(cat, "pins_list", nil)
	_, err := h(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	_, ok := cat.input[catalog.ReservedAuthTokenKey]
	assert.False(t, ok, "no injection when there is no credential resolver")
}
