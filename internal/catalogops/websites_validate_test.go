package catalogops

import (
	"context"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// fakeWebsitesService is a minimal websites.Service fake implementing only
// List and Validate; every other method is a no-op stub. It records the id
// passed to Validate so tests can assert ID and domain inputs reach the same
// validate call.
type fakeWebsitesService struct {
	websites.Service
	items         []ipfs.WebsiteItem
	validatedWith []string
}

func (f *fakeWebsitesService) RequireAuthenticated() error { return nil }
func (f *fakeWebsitesService) List(_ context.Context, _ websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	return f.items, nil
}
func (f *fakeWebsitesService) Validate(_ context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	f.validatedWith = append(f.validatedWith, id)
	return &ipfs.WebsiteValidateResponse{Valid: true, Message: "ok"}, nil
}

// validateDeps returns WebsitesDeps whose service() yields the given fake.
func validateDeps(t testing.TB, fake *fakeWebsitesService) WebsitesDeps {
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

// TestWebsiteValidateIDAndDomainResolveIdentically guards the invariant the
// customer-facing flows rely on: `websites validate <ID>` and
// `websites validate <domain>` must both resolve to the SAME numeric website
// ID and issue the identical validate call. A divergence here is what would
// otherwise surface as an ID-only failure (e.g. a 502 on the ID form).
func TestWebsiteValidateIDAndDomainResolveIdentically(t *testing.T) {
	items := []ipfs.WebsiteItem{
		{Id: 11, Domain: "example.test"},
	}

	// Numeric-ID input: ResolveWebsiteID must pass the raw ID straight through.
	res, err := websites.ResolveWebsiteID(context.Background(), &fakeWebsitesService{items: items}, "11")
	if err != nil {
		t.Fatalf("resolve ID: %v", err)
	}
	if res != "11" {
		t.Fatalf("numeric resolve: got %q, want 11", res)
	}

	// Domain input: ResolveWebsiteID must look it up and return the SAME ID.
	res, err = websites.ResolveWebsiteID(context.Background(), &fakeWebsitesService{items: items}, "example.test")
	if err != nil {
		t.Fatalf("resolve domain: %v", err)
	}
	if res != "11" {
		t.Fatalf("domain resolve: got %q, want 11", res)
	}

	// End-to-end: run the websitesValidate handler with the numeric ID and the
	// domain, and assert Validate is called with the identical ID both times.
	byID := &fakeWebsitesService{items: items}
	byDomain := &fakeWebsitesService{items: items}

	op := websitesValidate(validateDeps(t, byID))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{"website": "11"}); err != nil {
		t.Fatalf("validate by id: %v", err)
	}
	op = websitesValidate(validateDeps(t, byDomain))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{"website": "example.test"}); err != nil {
		t.Fatalf("validate by domain: %v", err)
	}

	if len(byID.validatedWith) != 1 || byID.validatedWith[0] != "11" {
		t.Fatalf("validate by id call: got %v, want [11]", byID.validatedWith)
	}
	if len(byDomain.validatedWith) != 1 || byDomain.validatedWith[0] != "11" {
		t.Fatalf("validate by domain call: got %v, want [11]", byDomain.validatedWith)
	}
}
