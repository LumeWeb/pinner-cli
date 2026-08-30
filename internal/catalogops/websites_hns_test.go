package catalogops

import (
	"context"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// namespaceCaptureService records the last create/update request so tests can
// assert the namespace (and other fields) the handler sent to the server.
type namespaceCaptureService struct {
	websites.Service

	createReq    *ipfs.WebsiteRequest
	updateReq    *ipfs.WebsiteUpdateRequest
	updateID     string
	platformRoot bool
}

func (f *namespaceCaptureService) RequireAuthenticated() error { return nil }

func (f *namespaceCaptureService) List(_ context.Context, _ websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	return []ipfs.WebsiteItem{{Id: 7, Domain: "example.test", TargetType: "ipfs"}}, nil
}

func (f *namespaceCaptureService) Get(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return &ipfs.WebsiteItem{Id: 7, Domain: "example.test", TargetType: "ipfs"}, nil
}

func (f *namespaceCaptureService) ListPlatformDomains(_ context.Context) (*ipfs.PlatformDomainListResponse, error) {
	// A platform root makes any subdomain parse as a platform claim; expose it
	// only when the test opts in via platformRoot.
	if f.platformRoot {
		return &ipfs.PlatformDomainListResponse{Total: 1, Data: []ipfs.PlatformDomainResponse{{
			Id: 1, Domain: "pin.xyz", Namespace: "icann", Enabled: true,
		}}}, nil
	}
	return &ipfs.PlatformDomainListResponse{}, nil
}

func (f *namespaceCaptureService) CreateWithOptions(_ context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	f.createReq = &req
	return &ipfs.WebsiteItem{Id: 1, Domain: derefStr(req.Domain), Status: "active"}, nil
}

func (f *namespaceCaptureService) UpdateWithOptions(_ context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	f.updateID = id
	f.updateReq = &req
	return &ipfs.WebsiteItem{Id: 7, Domain: "example.test", Status: "active"}, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func namespaceDeps(t testing.TB, fake *namespaceCaptureService) WebsitesDeps {
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

const testCID = "QmYwAPJzv5CZsnAzt8auVZRnXbW7Z5k7pZNeRp4cQ3vJdH"

// TestWebsitesCreate_HNSNamespace guards that a custom domain created with
// namespace=hns is sent to the backend on the WebsiteRequest. This is the
// first-class Handshake (alt-root) path for the primary create tool, used by
// both the CLI and the MCP tool surface.
func TestWebsitesCreate_HNSNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesCreate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website":   "acme",
		"namespace": "hns",
		"cid":       testCID,
	})
	require.NoError(t, err)
	require.NotNil(t, fake.createReq)
	require.Equal(t, "acme", derefStr(fake.createReq.Domain))
	require.NotNil(t, fake.createReq.Namespace)
	require.Equal(t, "hns", *fake.createReq.Namespace)
}

// TestWebsitesCreate_DefaultNamespace guards that omitting namespace defaults
// to icann on the custom-domain path (zero behavior change for ICANN users).
func TestWebsitesCreate_DefaultNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesCreate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "example.com",
		"cid":     testCID,
	})
	require.NoError(t, err)
	require.NotNil(t, fake.createReq)
	require.NotNil(t, fake.createReq.Namespace)
	require.Equal(t, "icann", *fake.createReq.Namespace)
}

// TestWebsitesCreate_PlatformClaimIgnoresNamespace guards that the platform
// subdomain path never sets a root namespace on the WebsiteRequest (platform
// subdomains derive their namespace from the platform root).
func TestWebsitesCreate_PlatformClaimIgnoresNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesCreate(namespaceDeps(t, fake))

	// No domain -> platform mint path.
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid": testCID,
	})
	require.NoError(t, err)
	require.NotNil(t, fake.createReq)
	require.Nil(t, fake.createReq.Namespace)
	require.Nil(t, fake.createReq.Domain)
}

// TestWebsitesCreate_InvalidNamespace guards that an unsupported namespace is
// rejected up front.
func TestWebsitesCreate_InvalidNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesCreate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website":   "example.com",
		"namespace": "foo",
		"cid":       testCID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be 'icann' or 'hns'")
}

// TestWebsitesUpdate_HNSNamespace guards that websites_update sends the
// namespace through to the backend when explicitly provided.
func TestWebsitesUpdate_HNSNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesUpdate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website":   "example.test",
		"namespace": "hns",
	})
	require.NoError(t, err)
	require.NotNil(t, fake.updateReq)
	require.NotNil(t, fake.updateReq.Namespace)
	require.Equal(t, "hns", *fake.updateReq.Namespace)
}

// TestWebsitesUpdate_NamespaceOmitted guards that an omitted namespace leaves
// req.Namespace nil (the server keeps the current namespace — no silent change).
func TestWebsitesUpdate_NamespaceOmitted(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesUpdate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "example.test",
	})
	require.NoError(t, err)
	require.NotNil(t, fake.updateReq)
	require.Nil(t, fake.updateReq.Namespace)
}

// TestWebsitesUpdate_InvalidNamespace guards rejection of an unsupported
// namespace on update.
func TestWebsitesUpdate_InvalidNamespace(t *testing.T) {
	fake := &namespaceCaptureService{}
	op := websitesUpdate(namespaceDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"website":   "example.test",
		"namespace": "foo",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be 'icann' or 'hns'")
}
