package catalogops

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// TestIPNSServiceAuthTokenPrecedence verifies that IPNSDeps.service honors the
// per-invocation --auth-token flag override (threaded through the input map)
// ahead of the GetAuthToken config fallback, mirroring PinsDeps.service.
func TestIPNSServiceAuthTokenPrecedence(t *testing.T) {
	var gotToken string
	d := IPNSDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, token string, _ bool) (ipns.Service, error) {
			gotToken = token
			return nil, nil
		},
		GetAuthToken: func() string { return "config-token" },
	}

	// Flag override wins.
	if _, err := d.service(map[string]any{AuthTokenInputKey: "flag-token"}); err != nil {
		t.Fatalf("service(flag): unexpected error: %v", err)
	}
	if gotToken != "flag-token" {
		t.Fatalf("flag override: got %q, want flag-token", gotToken)
	}

	// Falls back to GetAuthToken config token when no flag override.
	if _, err := d.service(map[string]any{}); err != nil {
		t.Fatalf("service(config): unexpected error: %v", err)
	}
	if gotToken != "config-token" {
		t.Fatalf("config fallback: got %q, want config-token", gotToken)
	}
}

// TestIPNSServiceNilConfigError verifies a nil config manager yields a clean
// error instead of a nil-pointer panic inside NewAuthenticated/ServiceFactory,
// mirroring PinsDeps.service's guard.
func TestIPNSServiceNilConfigError(t *testing.T) {
	d := IPNSDeps{
		// CfgMgr unset -> config() returns nil -> service() must error.
		NewAuthenticated: func(_ config.Manager, _ string, _ bool) (ipns.Service, error) {
			t.Fatal("NewAuthenticated must not be reached with a nil config manager")
			return nil, nil
		},
	}
	if _, err := d.service(map[string]any{}); err == nil {
		t.Fatal("service: expected error for nil config manager, got nil")
	}
}
