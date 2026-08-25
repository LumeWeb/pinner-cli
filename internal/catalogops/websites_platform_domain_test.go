package catalogops

import (
	"context"
	"strings"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// platformDomainService fakes websites.Service for the platform-domain
// operations, capturing the label / request each test exercises. It embeds the
// real interface so unimplemented methods panic rather than silently succeed.
type platformDomainService struct {
	websites.Service

	authErr                    error
	availabilityFn             func(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error)
	bindDomainFn               func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error)
	bindReqCapture             *ipfs.DomainRequest
	checkPlatformAvailabilityLabel string
}

func (f *platformDomainService) RequireAuthenticated() error { return f.authErr }

func (f *platformDomainService) List(_ context.Context) ([]ipfs.WebsiteItem, error) {
	return []ipfs.WebsiteItem{{Id: 11, Domain: "example.test"}}, nil
}

func (f *platformDomainService) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	f.checkPlatformAvailabilityLabel = label
	if f.availabilityFn != nil {
		return f.availabilityFn(ctx, label)
	}
	return nil, nil
}

func (f *platformDomainService) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	if f.bindDomainFn != nil {
		return f.bindDomainFn(ctx, websiteID, req)
	}
	return nil, nil
}

func platformDomainDeps(t testing.TB, fake *platformDomainService) WebsitesDeps {
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

func TestWebsitesPlatformDomainAvailability(t *testing.T) {
	fake := &platformDomainService{
		availabilityFn: func(_ context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
			return &ipfs.PlatformAvailabilityResponse{
				Label: label,
				Results: []ipfs.PlatformAvailabilityResult{
					{PlatformDomain: "ipfs.pin.xyz", Namespace: "icann", Available: true},
				},
			}, nil
		},
	}
	op := websitesPlatformDomainAvailability(platformDomainDeps(t, fake))

	res, err := op.Handler().Execute(context.Background(), map[string]any{"label": "my-app"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if fake.checkPlatformAvailabilityLabel != "my-app" {
		t.Fatalf("label = %q, want my-app", fake.checkPlatformAvailabilityLabel)
	}
	got, ok := res.(*ipfs.PlatformAvailabilityResponse)
	if !ok {
		t.Fatalf("result type = %T, want *ipfs.PlatformAvailabilityResponse", res)
	}
	if got.Label != "my-app" || len(got.Results) != 1 || !got.Results[0].Available {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestWebsitesPlatformDomainAvailabilityRequiresLabel(t *testing.T) {
	op := websitesPlatformDomainAvailability(platformDomainDeps(t, &platformDomainService{}))

	// The backend rejects an empty/missing label (422 "Label: is required"),
	// so the schema must require it for both the MCP tool and the CLI.
	labelArg := op.Args()[0]
	if !labelArg.Required {
		t.Fatalf("label arg Required = false, want true")
	}

	if _, err := catalog.NormalizeOperationInput(op, map[string]any{}); err == nil || !strings.Contains(err.Error(), `missing required argument "label"`) {
		t.Fatalf("catalog.NormalizeOperationInput({}) error = %v, want missing required argument \"label\"", err)
	}
}

func TestWebsitesDomainsAddPlatformDomainPassThrough(t *testing.T) {
	requireBindCapture := func(t *testing.T, req *ipfs.DomainRequest, generate *bool, label, platformDomain, platformNamespace string) {
		t.Helper()
		if req == nil {
			t.Fatal("BindDomain not invoked")
		}
		if (generate == nil) != (req.Generate == nil) {
			t.Fatalf("Generate presence = %v, want %v", req.Generate, generate)
		}
		if generate != nil && req.Generate != nil && *req.Generate != *generate {
			t.Fatalf("Generate = %v, want %v", *req.Generate, *generate)
		}
		if !strPtrEqual(req.Label, label) {
			t.Fatalf("Label = %v, want %v", req.Label, label)
		}
		if !strPtrEqual(req.PlatformDomain, platformDomain) {
			t.Fatalf("PlatformDomain = %v, want %v", req.PlatformDomain, platformDomain)
		}
		if !strPtrEqual(req.PlatformNamespace, platformNamespace) {
			t.Fatalf("PlatformNamespace = %v, want %v", req.PlatformNamespace, platformNamespace)
		}
	}

	for _, tc := range []struct {
		name              string
		input             map[string]any
		generate          *bool
		label             string
		platformDomain    string
		platformNamespace string
	}{
		{"explicit label claim", map[string]any{"website": "example.test", "domain": "my-app", "label": "my-app", "platform-domain": "ipfs.pin.xyz", "platform-namespace": "pin"}, nil, "my-app", "ipfs.pin.xyz", "pin"},
		{"generate flag", map[string]any{"website": "example.test", "domain": "my-app", "generate": true, "platform-domain": "ipfs.pin.xyz", "platform-namespace": "pin"}, boolPtr(true), "", "ipfs.pin.xyz", "pin"},
		{"no platform fields", map[string]any{"website": "example.test", "domain": "example.com"}, nil, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured *ipfs.DomainRequest
			fake := &platformDomainService{
				bindDomainFn: func(_ context.Context, _ string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					cp := req
					captured = &cp
					return &ipfs.DomainResponse{Id: 1, Domain: req.Domain}, nil
				},
			}
			op := websitesDomainsAdd(platformDomainDeps(t, fake))
			if _, err := op.Handler().Execute(context.Background(), tc.input); err != nil {
				t.Fatalf("handler: %v", err)
			}
			requireBindCapture(t, captured, tc.generate, tc.label, tc.platformDomain, tc.platformNamespace)
		})
	}
}

// strPtrEqual reports whether a *string equals a plain value (nil == "").
func strPtrEqual(p *string, s string) bool {
	if p == nil {
		return s == ""
	}
	return *p == s
}

func boolPtr(b bool) *bool { return &b }
