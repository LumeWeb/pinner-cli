package catalogops

import (
	"context"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/dns"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// TestWebsitesServiceAuthTokenPrecedence covers the websites domain, which
// returns (service, error) from service(). It verifies that the per-invocation
// --auth-token flag threaded through the input map takes precedence over the
// deps.GetAuthToken() config fallback.
func TestWebsitesServiceAuthTokenPrecedence(t *testing.T) {
	var gotToken string
	d := WebsitesDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, token string) (websites.Service, error) {
			gotToken = token
			return nil, nil
		},
		GetAuthToken: func() string { return "config-token" },
		ServiceFactory: func(_ config.Manager, _ bool, _ ...websites.Option) websites.Service {
			t.Fatal("ServiceFactory must not be used when an auth token override is present")
			return nil
		},
	}
	d.service(map[string]any{AuthTokenInputKey: "flag-token"})
	if gotToken != "flag-token" {
		t.Fatalf("flag override: got %q, want flag-token", gotToken)
	}
	d.service(map[string]any{})
	if gotToken != "config-token" {
		t.Fatalf("config fallback: got %q, want config-token", gotToken)
	}
}

// TestDNSServiceAuthTokenPrecedence covers the DNS domain.
func TestDNSServiceAuthTokenPrecedence(t *testing.T) {
	var gotToken string
	d := DNSDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, token string) dns.Service {
			gotToken = token
			return nil
		},
		GetAuthToken: func() string { return "config-token" },
		ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
			t.Fatal("ServiceFactory must not be used when an auth token override is present")
			return nil
		},
	}
	d.service(map[string]any{AuthTokenInputKey: "flag-token"})
	if gotToken != "flag-token" {
		t.Fatalf("flag override: got %q, want flag-token", gotToken)
	}
	d.service(map[string]any{})
	if gotToken != "config-token" {
		t.Fatalf("config fallback: got %q, want config-token", gotToken)
	}
}

// TestWebsitesServiceNilConfigError verifies a nil config manager yields a clean
// error instead of a nil-pointer panic inside ServiceFactory/NewAuthenticated.
func TestWebsitesServiceNilConfigError(t *testing.T) {
	d := WebsitesDeps{
		// CfgMgr unset -> config() returns nil -> service() must error.
		NewAuthenticated: func(_ config.Manager, _ bool, _ string) (websites.Service, error) {
			t.Fatal("NewAuthenticated must not be reached with a nil config manager")
			return nil, nil
		},
	}
	if _, err := d.service(map[string]any{}); err == nil {
		t.Fatal("service: expected error for nil config manager, got nil")
	}
}

// TestDNSServiceNilConfigError mirrors the websites guard for the DNS domain.
func TestDNSServiceNilConfigError(t *testing.T) {
	d := DNSDeps{
		// CfgMgr unset -> config() returns nil -> service() must error.
		NewAuthenticated: func(_ config.Manager, _ bool, _ string) dns.Service {
			t.Fatal("NewAuthenticated must not be reached with a nil config manager")
			return nil
		},
	}
	if _, err := d.service(map[string]any{}); err == nil {
		t.Fatal("service: expected error for nil config manager, got nil")
	}
}

// TestDestructiveDeleteConfirmGuards verifies the websites/dns delete handlers
// refuse to run without confirmation, even for human/app actors that bypass
// the CLI wiring gate (Catalog.Invoke only gates SafetyDestructive for model
// actors). Each handler must check input["confirm"] before touching the service.
func TestDestructiveDeleteConfirmGuards(t *testing.T) {
	ctx := context.Background()
	deps := WebsitesDeps{}
	for _, op := range WebsitesOperations(deps) {
		if op.Name() != "websites.delete" {
			continue
		}
		if _, err := op.Handler().Execute(ctx, map[string]any{}); err == nil {
			t.Fatal("websites.delete: expected confirmation error, got nil")
		}
	}
	dnsDeps := DNSDeps{}
	for _, op := range DNSOperations(dnsDeps) {
		if op.Name() != "dns.zones.delete" && op.Name() != "dns.records.delete" {
			continue
		}
		if _, err := op.Handler().Execute(ctx, map[string]any{}); err == nil {
			t.Fatalf("%s: expected confirmation error, got nil", op.Name())
		}
	}
}
