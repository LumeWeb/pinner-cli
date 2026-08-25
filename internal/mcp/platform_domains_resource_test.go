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
	resp       *ipfs.PlatformAvailabilityResponse
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
func (m *mockPlatformDomainsProvider) CheckPlatformDomainAvailability(_ context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	m.probeLabel = label
	return m.resp, m.err
}

func TestPlatformDomainsResource(t *testing.T) {
	prov := &mockPlatformDomainsProvider{
		resp: &ipfs.PlatformAvailabilityResponse{
			Label: "",
			Results: []ipfs.PlatformAvailabilityResult{
				{Available: true, Namespace: "icann", PlatformDomain: "ipfs.pin.xyz"},
				{Available: false, Namespace: "icann", PlatformDomain: "pin.xyz"},
			},
		},
	}

	handler := platformDomainsHandler(prov)
	res, err := handler(context.Background(), model.ResourceRequest{URI: PlatformDomainsURI})
	require.NoError(t, err)
	require.Equal(t, PlatformDomainsURI, res.URI)
	require.Equal(t, "application/json", res.MIMEType)

	// The handler probes with an empty label to list all roots.
	require.Equal(t, "", prov.probeLabel)

	var parsed ipfs.PlatformAvailabilityResponse
	require.NoError(t, json.Unmarshal([]byte(res.Text), &parsed))
	require.Len(t, parsed.Results, 2)
	require.Equal(t, "ipfs.pin.xyz", parsed.Results[0].PlatformDomain)
	require.True(t, parsed.Results[0].Available)
}

func TestPlatformDomainsResourceUnwired(t *testing.T) {
	handler := platformDomainsHandler(nil)
	_, err := handler(context.Background(), model.ResourceRequest{URI: PlatformDomainsURI})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}
