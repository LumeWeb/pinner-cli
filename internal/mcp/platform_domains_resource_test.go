package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// mockPlatformDomainsProvider implements wizard.WebsitesResourceProvider for
// the platform-domains resource test, returning a canned availability probe.
type mockPlatformDomainsProvider struct {
	probeLabel string
	resp       *ipfs.PlatformDomainListResponse
	err        error
}

func (m *mockPlatformDomainsProvider) GetByDomain(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return nil, nil
}
func (m *mockPlatformDomainsProvider) GetByID(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return nil, nil
}
func (m *mockPlatformDomainsProvider) Validate(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
	return nil, nil
}
func (m *mockPlatformDomainsProvider) GetConfig(_ context.Context) (*ipfs.WebsiteConfigResponse, error) {
	return nil, nil
}
func (m *mockPlatformDomainsProvider) ListPlatformDomains(_ context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return m.resp, m.err
}
func (m *mockPlatformDomainsProvider) CheckPlatformDomainAvailability(_ context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	m.probeLabel = label
	return nil, nil
}

func TestPlatformDomainsResource(t *testing.T) {
	prov := &mockPlatformDomainsProvider{
		resp: &ipfs.PlatformDomainListResponse{
			Total: 2,
			Data: []ipfs.PlatformDomainResponse{
				{Id: 1, Domain: "ipfs.pin.xyz", Namespace: "icann", ZoneId: 5, Enabled: true},
				{Id: 2, Domain: "pin.xyz", Namespace: "icann", ZoneId: 6, Enabled: false},
			},
		},
	}

	handler := platformDomainsHandler(prov)
	res, err := handler(context.Background(), model.ResourceRequest{URI: PlatformDomainsURI})
	require.NoError(t, err)
	require.Equal(t, PlatformDomainsURI, res.URI)
	require.Equal(t, "application/json", res.MIMEType)

	var parsed ipfs.PlatformDomainListResponse
	require.NoError(t, json.Unmarshal([]byte(res.Text), &parsed))
	require.Len(t, parsed.Data, 2)
	require.Equal(t, "ipfs.pin.xyz", parsed.Data[0].Domain)
	require.True(t, parsed.Data[0].Enabled)
}

func TestPlatformDomainsResourceUnwired(t *testing.T) {
	handler := platformDomainsHandler(nil)
	_, err := handler(context.Background(), model.ResourceRequest{URI: PlatformDomainsURI})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}
