package catalogops

import (
	"context"
	"testing"
)

// TestWebsitesCreateRequiresCustomDomainUnlessPlatformClaim guards that a
// websites_create with neither a custom domain nor a platform claim is rejected
// up front, so no orphan website can be created.
func TestWebsitesCreateRequiresCustomDomainUnlessPlatformClaim(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid": "QmX",
	})
	if err == nil {
		t.Fatal("expected an error when neither domain nor platform claim is provided")
	}
	if fake.createReq != nil {
		t.Fatal("CreateWithOptions must not be called when the request is rejected")
	}
}

// TestWebsitesCreatePlatformClaimRequiresLabelOrGenerate guards that a platform
// claim (--platform-domain) must provide exactly one of --label or --generate.
func TestWebsitesCreatePlatformClaimRequiresLabelOrGenerate(t *testing.T) {
	min := func(input map[string]any) error {
		fake := &dnsCapturingService{}
		op := websitesCreate(dnsCaptureDeps(t, fake))
		_, err := op.Handler().Execute(context.Background(), input)
		return err
	}
	if err := min(map[string]any{"cid": "QmX", "platform-domain": "pinned.site"}); err == nil {
		t.Fatal("expected error when platform claim has neither label nor generate")
	}
	// Both together is also invalid.
	if err := min(map[string]any{"cid": "QmX", "platform-domain": "pinned.site", "label": "myapp", "generate": true}); err == nil {
		t.Fatal("expected error when platform claim has both label and generate")
	}
}

// TestWebsitesCreatePlatformClaimGenerate asserts that a generate-based platform
// claim sends the claim fields directly to CreateWithOptions and omits Domain.
func TestWebsitesCreatePlatformClaimGenerate(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid":             "QmX",
		"platform-domain": "pinned.site",
		"generate":        true,
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	req := fake.createReq
	if req == nil {
		t.Fatal("CreateWithOptions not invoked")
	}
	if req.Domain != nil {
		t.Fatalf("Domain = %q, want nil for a generated platform subdomain", *req.Domain)
	}
	if req.PlatformDomain == nil || *req.PlatformDomain != "pinned.site" {
		t.Fatalf("PlatformDomain = %v, want pinned.site", req.PlatformDomain)
	}
	if req.Generate == nil || !*req.Generate {
		t.Fatalf("Generate = %v, want true", req.Generate)
	}
	if req.Label != nil {
		t.Fatalf("Label = %q, want nil for generate", *req.Label)
	}
	if req.DnsHostingEnabled == nil || !*req.DnsHostingEnabled {
		t.Fatalf("DnsHostingEnabled = %v, want true (platform subdomains are managed)", req.DnsHostingEnabled)
	}
}

// TestWebsitesCreatePlatformClaimLabel asserts that a label-based platform claim
// sends the label + platform-domain directly to CreateWithOptions and omits
// Domain and Generate.
func TestWebsitesCreatePlatformClaimLabel(t *testing.T) {
	fake := &dnsCapturingService{}
	op := websitesCreate(dnsCaptureDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"cid":               "QmX",
		"platform-domain":   "pinned.site",
		"platform-namespace": "hns",
		"label":             "myapp",
	})
	if err != nil {
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
	if req.PlatformNamespace == nil || *req.PlatformNamespace != "hns" {
		t.Fatalf("PlatformNamespace = %v, want hns", req.PlatformNamespace)
	}
	if req.Label == nil || *req.Label != "myapp" {
		t.Fatalf("Label = %v, want myapp", req.Label)
	}
	if req.Generate != nil {
		t.Fatalf("Generate = %v, want nil for label claim", req.Generate)
	}
	if req.DnsHostingEnabled == nil || !*req.DnsHostingEnabled {
		t.Fatalf("DnsHostingEnabled = %v, want true (platform subdomains are managed)", req.DnsHostingEnabled)
	}
}
