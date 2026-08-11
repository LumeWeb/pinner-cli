package catalogops

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/config"
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
