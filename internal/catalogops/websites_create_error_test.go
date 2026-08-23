package catalogops

import (
	"context"
	"errors"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// errWebsitesService is a websites.Service fake whose CreateWithOptions returns
// the configured error; every other method is a no-op stub.
type errWebsitesService struct {
	websites.Service
	err error
}

func (f *errWebsitesService) RequireAuthenticated() error { return nil }
func (f *errWebsitesService) CreateWithOptions(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	return nil, f.err
}
func (f *errWebsitesService) UpdateWithOptions(_ context.Context, _ string, _ ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	return nil, f.err
}
func (f *errWebsitesService) Validate(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
	return nil, f.err
}

// errDeps returns WebsitesDeps whose service() yields the given fake.
func errDeps(t testing.TB, fake *errWebsitesService) WebsitesDeps {
	return WebsitesDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, _ string) (websites.Service, error) {
			return fake, nil
		},
		ServiceFactory: func(_ config.Manager, _ bool, _ ...websites.Option) websites.Service {
			return fake
		},
		GetAuthToken: func() string { return "" },
	}
}

// TestWebsitesCreateTranslatesBackendReason guards that a websites-create
// failure carrying a backend reason code (e.g. the target CID not being pinned)
// is surfaced as a clear, actionable message to the caller. The catalog
// websitesCreate handler is used by BOTH the CLI (`websites create`) and the
// MCP tool-call surface, so this single seam covers both.
func TestWebsitesCreateTranslatesBackendReason(t *testing.T) {
	base := errors.New("invalid website data")
	fake := &errWebsitesService{err: &ipfs.APIError{Reason: ipfs.ErrorCodeCIDNotPinned, Err: base}}

	op := websitesCreate(errDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "example.test",
		"cid":     "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not pinned on the gateway")
	require.Contains(t, err.Error(), "pin it first")
	// Original chain preserved for errors.Is.
	require.ErrorIs(t, err, base)
}
