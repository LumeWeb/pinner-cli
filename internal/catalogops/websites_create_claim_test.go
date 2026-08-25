package catalogops

import (
	"context"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// TestWebsitesCreateDefaultsToMintedPlatform verifies that a websites_create
// with no domain and no claim fields defaults to a minted platform subdomain:
// Domain is omitted, generate=true, and DNS hosting is force-enabled.
func TestWebsitesCreateDefaultsToMintedPlatform(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid": "QmX",
	}); err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain != nil {
		t.Fatalf("Domain = %q, want nil for a minted platform subdomain", *req.Domain)
	}
	if req.Generate == nil || !*req.Generate {
		t.Fatalf("Generate = %v, want true (default mint)", req.Generate)
	}
	if req.DnsHostingEnabled == nil || !*req.DnsHostingEnabled {
		t.Fatalf("DnsHostingEnabled = %v, want true (platform subdomains are managed)", req.DnsHostingEnabled)
	}
}

// TestWebsitesCreateCustomDomain verifies a non-platform domain is treated as a
// custom domain: req.Domain is set, no platform claim fields, DNS-hosting is
// passed through (omitted -> nil / backend default).
func TestWebsitesCreateCustomDomain(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "example.com",
		"cid":     "QmX",
	}); err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain == nil || *req.Domain != "example.com" {
		t.Fatalf("Domain = %v, want example.com", req.Domain)
	}
	if req.PlatformDomain != nil || req.Label != nil || req.Generate != nil {
		t.Fatalf("custom domain must not carry platform claim fields: %+v", req)
	}
	if req.DnsHostingEnabled != nil {
		t.Fatalf("DnsHostingEnabled = %v, want nil (backend default) when dns-hosting is omitted", req.DnsHostingEnabled)
	}
}

// TestWebsitesCreatePlatformDomainsByParse verifies that a domain which is a
// subdomain of an enabled platform root is detected by parsing and claimed as a
// platform subdomain (label/root/namespace derived, Domain omitted).
func TestWebsitesCreatePlatformDomainsByParse(t *testing.T) {
	fake := &dnsCapturingService{
		platformDomains: []ipfs.PlatformDomainResponse{
			{Domain: "pinned.site", Enabled: true, Namespace: "icann"},
			{Domain: "disabled.site", Enabled: false, Namespace: "icann"},
		},
	}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "myapp.pinned.site",
		"cid":     "QmX",
	}); err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain != nil {
		t.Fatalf("Domain = %q, want nil for a parsed platform subdomain", *req.Domain)
	}
	if req.PlatformDomain == nil || *req.PlatformDomain != "pinned.site" {
		t.Fatalf("PlatformDomain = %v, want pinned.site (derived by parsing)", req.PlatformDomain)
	}
	if req.PlatformNamespace == nil || *req.PlatformNamespace != "icann" {
		t.Fatalf("PlatformNamespace = %v, want icann (from the platform root)", req.PlatformNamespace)
	}
	if req.Label == nil || *req.Label != "myapp" {
		t.Fatalf("Label = %v, want myapp (derived by parsing)", req.Label)
	}
}

// TestWebsitesCreatePlatformDomainsByParseCaseInsensitive verifies that
// platform-root detection is case-insensitive: DNS labels are case-insensitive,
// so a mixed-case hostname must still be claimed as a platform subdomain rather
// than falling through to the custom-domain branch.
func TestWebsitesCreatePlatformDomainsByParseCaseInsensitive(t *testing.T) {
	fake := &dnsCapturingService{
		platformDomains: []ipfs.PlatformDomainResponse{
			{Domain: "pinned.site", Enabled: true, Namespace: "icann"},
		},
	}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"website": "MyApp.Pinned.Site",
		"cid":     "QmX",
	}); err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain != nil {
		t.Fatalf("Domain = %q, want nil (mixed-case hostname must parse as a platform subdomain)", *req.Domain)
	}
	if req.PlatformDomain == nil || *req.PlatformDomain != "pinned.site" {
		t.Fatalf("PlatformDomain = %v, want pinned.site", req.PlatformDomain)
	}
	if req.Label == nil || *req.Label != "myapp" {
		t.Fatalf("Label = %v, want myapp (lowercased)", req.Label)
	}
	if req.Generate != nil {
		t.Fatalf("Generate = %v, want nil for a parsed label claim", req.Generate)
	}
	if req.DnsHostingEnabled == nil || !*req.DnsHostingEnabled {
		t.Fatalf("DnsHostingEnabled = %v, want true (platform subdomains are managed)", req.DnsHostingEnabled)
	}
}

// TestWebsitesCreateExplicitPlatformLabel verifies the agent path: platform:true
// plus an explicit label under an explicit platform domain, with no domain
// positional.
func TestWebsitesCreateExplicitPlatformLabel(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid":             "QmX",
		"platform":        true,
		"platform-domain": "pinned.site",
		"label":           "myapp",
	}); err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain != nil {
		t.Fatalf("Domain = %q, want nil for a platform subdomain claim", *req.Domain)
	}
	if req.PlatformDomain == nil || *req.PlatformDomain != "pinned.site" {
		t.Fatalf("PlatformDomain = %v, want pinned.site", req.PlatformDomain)
	}
	if req.Label == nil || *req.Label != "myapp" {
		t.Fatalf("Label = %v, want myapp", req.Label)
	}
}

// TestSubdomainLabel guards the case-insensitive platform-root label matcher
// that backs website-create's domain parsing.
func TestSubdomainLabel(t *testing.T) {
	cases := []struct {
		domain, root, want string
	}{
		{"myapp.pinned.site", "pinned.site", "myapp"},
		{"MyApp.Pinned.Site", "Pinned.Site", "myapp"}, // case-insensitive
		{"example.com", "pinned.site", ""},             // not a subdomain
		{"pinned.site", "pinned.site", ""},             // the bare root itself
		{"a.b.pinned.site", "b.pinned.site", "a"},      // deeper root
	}
	for _, tc := range cases {
		if got := subdomainLabel(tc.domain, tc.root); got != tc.want {
			t.Errorf("subdomainLabel(%q, %q) = %q, want %q", tc.domain, tc.root, got, tc.want)
		}
	}
}

// TestWebsitesCreateRejectsDomainWithPlatform guards that a custom-looking
// domain supplied together with platform intent is rejected up front, so the
// request is not doubly-addressed and the user's --dns-hosting choice is not
// silently dropped.
func TestWebsitesCreateRejectsDomainWithPlatform(t *testing.T) {
	min := func(input map[string]any) error {
		fake := &dnsCapturingService{}
		op := websitesCreate(dnsCaptureDeps(t, fake))
		_, err := op.Handler().Execute(context.Background(), input)
		return err
	}
	if err := min(map[string]any{
		"cid": "QmX", "website": "example.com",
		"platform": true, "dns-hosting": false,
	}); err == nil {
		t.Fatal("expected error when a custom domain and platform intent are both supplied")
	}
}

// TestWebsitesCreateRejectsLabelAndGenerate guards that label and generate are
// mutually exclusive on a platform claim.
func TestWebsitesCreateRejectsLabelAndGenerate(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid": "QmX", "platform": true, "label": "myapp", "generate": true,
	}); err == nil {
		t.Fatal("expected error when both label and generate are supplied")
	}
}
