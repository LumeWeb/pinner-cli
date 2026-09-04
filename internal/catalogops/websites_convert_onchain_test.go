package catalogops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// convertOnChainCaptureService satisfies websites.Service for the
// websites_domains_convert_onchain operation: it serves a one-website list
// with one HNS binding and records the website/domain IDs the handler called
// the SDK with.
type convertOnChainService struct {
	websites.Service

	websiteID string
	domainID  string
	called    bool
}

func (f *convertOnChainService) RequireAuthenticated() error { return nil }

func (f *convertOnChainService) List(_ context.Context, _ websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	return []ipfs.WebsiteItem{{Id: 7, Domain: "example.test", TargetType: "ipfs"}}, nil
}

func (f *convertOnChainService) ListDomains(_ context.Context, _ string) ([]ipfs.DomainResponse, error) {
	return []ipfs.DomainResponse{{
		Id:        3,
		Domain:    "acme",
		Namespace: ipfs.DomainNamespaceHNS,
		Status:    new(ipfs.DomainResponseStatusOnchainManaged),
	}}, nil
}

func (f *convertOnChainService) ConvertDomainToOnChain(_ context.Context, websiteID, domainID string) (*ipfs.DomainResponse, error) {
	f.called = true
	f.websiteID = websiteID
	f.domainID = domainID
	return &ipfs.DomainResponse{Id: 3, Domain: "acme", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusOnchainManaged)}, nil
}

func convertOnChainDeps(t testing.TB, fake *convertOnChainService) WebsitesDeps {
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

// TestWebsitesDomainsConvertOnChain_RequiresConfirm guards the destructive
// gate: the one-way conversion refuses to run without confirm=true.
func TestWebsitesDomainsConvertOnChain_RequiresConfirm(t *testing.T) {
	fake := &convertOnChainService{}
	op := websitesDomainsConvertOnChain(convertOnChainDeps(t, fake))

	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"domain": "acme",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "confirmation is required")
	require.False(t, fake.called, "the SDK conversion must never run without confirm")
}

// TestWebsitesDomainsConvertOnChain_WithConfirm guards the happy path: the op
// resolves the binding by name, sends the website + numeric binding IDs to the
// SDK, and returns the (onchain_managed) domain response.
func TestWebsitesDomainsConvertOnChain_WithConfirm(t *testing.T) {
	fake := &convertOnChainService{}
	op := websitesDomainsConvertOnChain(convertOnChainDeps(t, fake))

	result, err := op.Handler().Execute(context.Background(), map[string]any{
		"domain":  "acme",
		"confirm": true,
	})
	require.NoError(t, err)
	require.True(t, fake.called)
	require.Equal(t, "7", fake.websiteID)
	require.Equal(t, "3", fake.domainID)

	domain, ok := result.(*ipfs.DomainResponse)
	require.True(t, ok)
	require.NotNil(t, domain.Status)
	require.Equal(t, ipfs.DomainResponseStatusOnchainManaged, *domain.Status)
}

// TestWebsitesDomainsConvertOnChain_RegisteredCatalogOp guards that the op is
// part of the canonical websites catalog so both frontends (CLI + MCP) expose
// it — a missing entry here would silently drop the tool from one surface.
func TestWebsitesDomainsConvertOnChain_RegisteredCatalogOp(t *testing.T) {
	d := WebsitesDeps{
		CfgMgr: func() config.Manager { return nil },
		NewAuthenticated: func(_ config.Manager, _ bool, _ string) (websites.Service, error) {
			return nil, nil
		},
		ServiceFactory: func(_ config.Manager, _ bool, _ ...websites.Option) websites.Service {
			return nil
		},
		GetAuthToken: func() string { return "" },
	}
	names := make(map[string]bool)
	for _, op := range WebsitesOperations(d) {
		names[op.Name()] = true
	}
	require.True(t, names["websites_domains_convert_onchain"],
		"websites_domains_convert_onchain must be in WebsitesOperations")
}
