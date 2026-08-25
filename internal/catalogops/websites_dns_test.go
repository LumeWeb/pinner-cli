package catalogops

import (
	"context"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// dnsCapturingService fakes websites.Service to record the request passed to
// CreateWithOptions / UpdateWithOptions so tests can assert the dns-hosting
// tri-state mapping onto DnsHostingEnabled.
type dnsCapturingService struct {
	websites.Service
	createReq      *ipfs.WebsiteRequest
	updateReq      *ipfs.WebsiteUpdateRequest
	platformDomains []ipfs.PlatformDomainResponse
}

func (f *dnsCapturingService) RequireAuthenticated() error { return nil }

func (f *dnsCapturingService) ListPlatformDomains(_ context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return &ipfs.PlatformDomainListResponse{Data: f.platformDomains}, nil
}

func (f *dnsCapturingService) Get(_ context.Context, id string) (*ipfs.WebsiteItem, error) {
	return &ipfs.WebsiteItem{Id: 11, Domain: "example.test"}, nil
}

func (f *dnsCapturingService) CreateWithOptions(_ context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	f.createReq = &req
	return &ipfs.WebsiteItem{Id: 11}, nil
}

func (f *dnsCapturingService) UpdateWithOptions(_ context.Context, _ string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	f.updateReq = &req
	return &ipfs.WebsiteItem{Id: 11}, nil
}

func dnsCaptureDeps(t testing.TB, fake *dnsCapturingService) WebsitesDeps {
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

func TestWebsitesCreateDNSHostingTriState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   map[string]any
		wantNil bool
		wantVal bool
	}{
		{"omit -> nil (backend default)", map[string]any{"website": "example.test", "cid": "QmX"}, true, false},
		{"json null -> nil", map[string]any{"website": "example.test", "cid": "QmX", "dns-hosting": nil}, true, false},
		{"true -> managed", map[string]any{"website": "example.test", "cid": "QmX", "dns-hosting": true}, false, true},
		{"false -> self-managed", map[string]any{"website": "example.test", "cid": "QmX", "dns-hosting": false}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &dnsCapturingService{}
			op := websitesCreate(dnsCaptureDeps(t, fake))
			if _, err := op.Handler().Execute(context.Background(), tc.input); err != nil {
				t.Fatalf("create handler: %v", err)
			}
			if fake.createReq == nil {
				t.Fatal("create handler did not invoke CreateWithOptions")
			}
			switch {
			case tc.wantNil && fake.createReq.DnsHostingEnabled != nil:
				t.Fatalf("DnsHostingEnabled = %v, want nil (backend default)", *fake.createReq.DnsHostingEnabled)
			case !tc.wantNil && fake.createReq.DnsHostingEnabled == nil:
				t.Fatalf("DnsHostingEnabled = nil, want <%v>", tc.wantVal)
			case !tc.wantNil && *fake.createReq.DnsHostingEnabled != tc.wantVal:
				t.Fatalf("DnsHostingEnabled = %v, want %v", *fake.createReq.DnsHostingEnabled, tc.wantVal)
			}
		})
	}
}

func TestWebsitesUpdateDNSHostingTriState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   map[string]any
		wantNil bool
		wantVal bool
	}{
		{"omit -> nil (unchanged)", map[string]any{"website": "1", "rename-to": "x.test"}, true, false},
		{"true -> managed", map[string]any{"website": "1", "rename-to": "x.test", "dns-hosting": true}, false, true},
		{"false -> self-managed", map[string]any{"website": "1", "rename-to": "x.test", "dns-hosting": false}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &dnsCapturingService{}
			op := websitesUpdate(dnsCaptureDeps(t, fake))
			if _, err := op.Handler().Execute(context.Background(), tc.input); err != nil {
				t.Fatalf("update handler: %v", err)
			}
			if fake.updateReq == nil {
				t.Fatal("update handler did not invoke UpdateWithOptions")
			}
			switch {
			case tc.wantNil && fake.updateReq.DnsHostingEnabled != nil:
				t.Fatalf("DnsHostingEnabled = %v, want nil (unchanged)", *fake.updateReq.DnsHostingEnabled)
			case !tc.wantNil && fake.updateReq.DnsHostingEnabled == nil:
				t.Fatalf("DnsHostingEnabled = nil, want <%v>", tc.wantVal)
			case !tc.wantNil && *fake.updateReq.DnsHostingEnabled != tc.wantVal:
				t.Fatalf("DnsHostingEnabled = %v, want %v", *fake.updateReq.DnsHostingEnabled, tc.wantVal)
			}
		})
	}
}
