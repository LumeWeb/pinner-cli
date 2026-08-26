package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// mcpErrWebsitesService is a websites.Service fake whose CreateWithOptions
// returns the configured error. ListPlatformDomains returns an empty response
// so the create-with-custom-domain path does not hit the embedded nil
// interface; CreateWithOptions carries the translated error under test.
type mcpErrWebsitesService struct {
	websites.Service
	err error
}

func (f *mcpErrWebsitesService) RequireAuthenticated() error { return nil }
func (f *mcpErrWebsitesService) ListPlatformDomains(context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return nil, nil
}
func (f *mcpErrWebsitesService) CreateWithOptions(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	return nil, f.err
}

// mcpErrWebsitesDeps returns WebsitesDeps whose ServiceFactory yields the given
// fake. The config manager is only required to be non-nil (d.service() never
// reads config on this path because Secure and GetAuthToken are supplied).
func mcpErrWebsitesDeps(t *testing.T, fake *mcpErrWebsitesService) catalogops.WebsitesDeps {
	t.Helper()
	return catalogops.WebsitesDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		Secure: func() bool { return true },
		ServiceFactory: func(_ config.Manager, _ bool, _ ...websites.Option) websites.Service {
			return fake
		},
		GetAuthToken: func() string { return "" },
	}
}

// TestWebsitesCreateDispatchSurfacesTranslatedError guards the MCP tool-call
// half of the contract: dispatching the websites_create tool (built from the
// shared catalog op) against a service that fails with a CID_NOT_PINNED reason
// code must surface a translated, actionable error in ToolResult.Text with
// IsError set. The catalog websitesCreate handler is the same one the CLI uses,
// so this proves the MCP surface benefits from the shared translation seam in
// websites.TranslateError.
func TestWebsitesCreateDispatchSurfacesTranslatedError(t *testing.T) {
	base := errors.New("invalid website data")
	fake := &mcpErrWebsitesService{err: &ipfs.APIError{Reason: ipfs.ErrorCodeCIDNotPinned, Err: base}}

	var op catalog.Operation
	for _, o := range catalogops.WebsitesOperations(mcpErrWebsitesDeps(t, fake)) {
		if o.Name() == "websites_create" {
			op = o
			break
		}
	}
	require.NotNil(t, op, "websites_create operation not found")

	cat := catalog.NewCatalog()
	require.NoError(t, cat.Add(op))

	res, err := DispatchCatalogOp(context.Background(), cat, catalog.ActorModel, op.Name(), map[string]any{
		"website": "example.test",
		"cid":     "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
	}, op.Name())
	require.NoError(t, err)
	require.True(t, res.IsError, "expected an error result")
	require.Contains(t, res.Text, "not pinned on the gateway")
	require.Contains(t, res.Text, "pin it first")
}
