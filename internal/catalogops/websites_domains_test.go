package catalogops

import (
	"context"
	"errors"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// domainsService mocks websites.Service for the websites_domains_* handlers,
// overriding only the methods each test exercises. It embeds the real
// interface so unimplemented methods panic rather than silently succeed.
type domainsService struct {
	websites.Service

	authErr                error
	listFn                 func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	listDomainsFn          func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error)
	bindDomainFn           func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error)
	unbindDomainFn         func(ctx context.Context, websiteID string, domainID string) error
	verifyDomainFn         func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	dnsRequirementsFn      func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	republishDANEFn        func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error)
	updateDomainFn         func(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error)
	updateDomainReqCapture *ipfs.DomainUpdateRequest
}

func (f *domainsService) RequireAuthenticated() error { return f.authErr }

func (f *domainsService) List(ctx context.Context, opts websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, nil
}

func (f *domainsService) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	if f.listDomainsFn != nil {
		return f.listDomainsFn(ctx, websiteID)
	}
	return nil, nil
}

func (f *domainsService) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	if f.bindDomainFn != nil {
		return f.bindDomainFn(ctx, websiteID, req)
	}
	return nil, nil
}

func (f *domainsService) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	if f.unbindDomainFn != nil {
		return f.unbindDomainFn(ctx, websiteID, domainID)
	}
	return nil
}

func (f *domainsService) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if f.verifyDomainFn != nil {
		return f.verifyDomainFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (f *domainsService) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if f.dnsRequirementsFn != nil {
		return f.dnsRequirementsFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (f *domainsService) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	if f.republishDANEFn != nil {
		return f.republishDANEFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (f *domainsService) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	f.updateDomainReqCapture = &req
	if f.updateDomainFn != nil {
		return f.updateDomainFn(ctx, websiteID, domainID, req)
	}
	return nil, nil
}

func domainsDeps(t testing.TB, fake *domainsService) WebsitesDeps {
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

// singleWebsiteFixture returns a fixture with one website (id 7) having one
// bound domain (id 3, "example.test"), wired so ResolveDomainBinding and
// ResolveWebsiteID work.
func singleWebsiteFixture() *domainsService {
	return &domainsService{
		listFn: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.test"}}, nil
		},
		listDomainsFn: func(_ context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return []ipfs.DomainResponse{{Id: 3, Domain: "example.test", Namespace: ipfs.DomainNamespaceICANN}}, nil
		},
	}
}

func TestWebsitesDomainsListResolvesWebsite(t *testing.T) {
	fake := singleWebsiteFixture()
	var gotWebsiteID string
	fake.listDomainsFn = func(_ context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		gotWebsiteID = websiteID
		return []ipfs.DomainResponse{{Id: 3, Domain: "example.test", Namespace: ipfs.DomainNamespaceICANN}}, nil
	}

	op := websitesDomainsList(domainsDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"website": "example.test"})
	if err != nil {
		t.Fatalf("list handler: %v", err)
	}
	if gotWebsiteID != "7" {
		t.Fatalf("ListDomains websiteID = %q, want \"7\"", gotWebsiteID)
	}
	domains, ok := res.([]ipfs.DomainResponse)
	if !ok {
		t.Fatalf("result type = %T, want []ipfs.DomainResponse", res)
	}
	if len(domains) != 1 || domains[0].Id != 3 || domains[0].Domain != "example.test" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
}

func TestWebsitesDomainsListRequiresWebsite(t *testing.T) {
	fake := singleWebsiteFixture()
	op := websitesDomainsList(domainsDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected error when website arg is missing")
	}
}

func TestWebsitesDomainsRemoveResolvesBindingAndResult(t *testing.T) {
	fake := singleWebsiteFixture()
	var gotWebsiteID, gotDomainID string
	fake.unbindDomainFn = func(_ context.Context, websiteID string, domainID string) error {
		gotWebsiteID, gotDomainID = websiteID, domainID
		return nil
	}

	op := websitesDomainsRemove(domainsDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"domain": "example.test"})
	if err != nil {
		t.Fatalf("remove handler: %v", err)
	}
	if gotWebsiteID != "7" || gotDomainID != "3" {
		t.Fatalf("UnbindDomain(%q, %q), want (\"7\", \"3\")", gotWebsiteID, gotDomainID)
	}
	result, ok := res.(*WebsiteDomainsRemoveResult)
	if !ok {
		t.Fatalf("result type = %T, want *WebsiteDomainsRemoveResult", res)
	}
	if !result.Deleted || result.DomainID != "3" {
		t.Fatalf("unexpected remove result: %+v", result)
	}
}

func TestWebsitesDomainsAddRejectsInvalidNamespace(t *testing.T) {
	fake := singleWebsiteFixture()
	var bindCalled bool
	fake.bindDomainFn = func(_ context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
		bindCalled = true
		return &ipfs.DomainResponse{Id: 9, Domain: req.Domain, Namespace: ipfs.DomainNamespace(req.Namespace)}, nil
	}

	op := websitesDomainsAdd(domainsDeps(t, fake))
	for _, ns := range []string{"foo", "ICANN", "HNS", "icann ", "hns ", "ipfs"} {
		input := map[string]any{"website": "7", "domain": "example.test", "namespace": ns}
		if _, err := op.Handler().Execute(context.Background(), input); err == nil {
			t.Fatalf("namespace %q: want error, got nil (BindDomain must not be called)", ns)
		}
	}
	if bindCalled {
		t.Fatal("BindDomain was called with an invalid namespace; validation must reject before the service")
	}
}

func TestWebsitesDomainsAddValidNamespaceReachesBindDomain(t *testing.T) {
	fake := singleWebsiteFixture()
	var gotReq ipfs.DomainRequest
	fake.bindDomainFn = func(_ context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
		gotReq = req
		return &ipfs.DomainResponse{Id: 9, Domain: req.Domain, Namespace: ipfs.DomainNamespace(req.Namespace)}, nil
	}

	op := websitesDomainsAdd(domainsDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"website": "7", "domain": "example.test", "namespace": "hns"})
	if err != nil {
		t.Fatalf("add handler with hns namespace: %v", err)
	}
	if gotReq.Namespace != "hns" {
		t.Fatalf("BindDomain namespace = %q, want hns", gotReq.Namespace)
	}
	if _, ok := res.(*ipfs.DomainResponse); !ok {
		t.Fatalf("result type = %T, want *ipfs.DomainResponse", res)
	}
}

func TestWebsitesDomainsDANERepublishTypedResult(t *testing.T) {
	fake := singleWebsiteFixture()
	fake.republishDANEFn = func(_ context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
		if websiteID != "7" || domainID != "3" {
			t.Fatalf("RepublishDANE(%q, %q), want (\"7\", \"3\")", websiteID, domainID)
		}
		tlsa := "_443._tcp.example.test. 60 IN TLSA 3 1 1 abc123"
		return &ipfs.DomainDANERepublishResponse{Id: 3, Domain: "example.test", TlsaRecord: &tlsa}, nil
	}

	op := websitesDomainsDANERepublish(domainsDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"domain": "example.test"})
	if err != nil {
		t.Fatalf("dane republish handler: %v", err)
	}
	result, ok := res.(*ipfs.DomainDANERepublishResponse)
	if !ok {
		t.Fatalf("result type = %T, want *ipfs.DomainDANERepublishResponse", res)
	}
	if result.Id != 3 || result.Domain != "example.test" {
		t.Fatalf("unexpected dane republish result: %+v", result)
	}
}

func TestWebsitesDomainsRequiresAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   func(WebsitesDeps) catalog.Operation
	}{
		{"list", func(d WebsitesDeps) catalog.Operation { return websitesDomainsList(d) }},
		{"remove", func(d WebsitesDeps) catalog.Operation { return websitesDomainsRemove(d) }},
		{"dane_republish", func(d WebsitesDeps) catalog.Operation { return websitesDomainsDANERepublish(d) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := singleWebsiteFixture()
			fake.authErr = errors.New("not authenticated")
			op := tc.op(domainsDeps(t, fake))
			if _, err := op.Handler().Execute(context.Background(), map[string]any{"website": "7", "domain": "example.test"}); err == nil || err.Error() != "not authenticated" {
				t.Fatalf("err = %v, want auth error", err)
			}
		})
	}
}

// TestWebsitesDomainsUpdateNullability guards the nullable-bool regression:
// an explicit --dns-hosting=false must map to DnsHostingEnabled=&false (a real
// disable), not a silent no-op, and primary=false likewise maps to Primary=&false.
func TestWebsitesDomainsUpdateNullability(t *testing.T) {
	fake := singleWebsiteFixture()
	op := websitesDomainsUpdate(domainsDeps(t, fake))

	// Explicit false on the enable form => disable (maps to no-dns-hosting).
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"domain": "example.test", "dns-hosting": new(false),
	}); err != nil {
		t.Fatalf("dns-hosting=false should not error, got: %v", err)
	}
	if fake.updateDomainReqCapture == nil || fake.updateDomainReqCapture.DnsHostingEnabled == nil {
		t.Fatal("expected DnsHostingEnabled to be set for explicit dns-hosting=false")
	}
	if *fake.updateDomainReqCapture.DnsHostingEnabled != false {
		t.Errorf("DnsHostingEnabled = %v, want false", *fake.updateDomainReqCapture.DnsHostingEnabled)
	}

	// primary=false => Primary=&false.
	fake2 := singleWebsiteFixture()
	op2 := websitesDomainsUpdate(domainsDeps(t, fake2))
	if _, err := op2.Handler().Execute(context.Background(), map[string]any{
		"domain": "example.test", "primary": new(false),
	}); err != nil {
		t.Fatalf("primary=false should not error, got: %v", err)
	}
	if fake2.updateDomainReqCapture == nil || fake2.updateDomainReqCapture.Primary == nil {
		t.Fatal("expected Primary to be set for explicit primary=false")
	}
	if *fake2.updateDomainReqCapture.Primary != false {
		t.Errorf("Primary = %v, want false", *fake2.updateDomainReqCapture.Primary)
	}

	// True forms still work.
	fake3 := singleWebsiteFixture()
	op3 := websitesDomainsUpdate(domainsDeps(t, fake3))
	if _, err := op3.Handler().Execute(context.Background(), map[string]any{
		"domain": "example.test", "dns-hosting": new(true), "primary": new(true),
	}); err != nil {
		t.Fatalf("dns-hosting=true primary=true should not error, got: %v", err)
	}
	if fake3.updateDomainReqCapture == nil ||
		fake3.updateDomainReqCapture.DnsHostingEnabled == nil ||
		fake3.updateDomainReqCapture.Primary == nil ||
		*fake3.updateDomainReqCapture.DnsHostingEnabled != true ||
		*fake3.updateDomainReqCapture.Primary != true {
		t.Errorf("expected DnsHostingEnabled=true Primary=true, got: %+v", fake3.updateDomainReqCapture)
	}
}


