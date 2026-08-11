package catalogops

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
)

// TestAuthTokenFromInput verifies the auth_token input-key override is read
// only when present and non-empty, mirroring the legacy flag -> config
// precedence at the input layer.
func TestAuthTokenFromInput(t *testing.T) {
	if got := authTokenFromInput(nil); got != "" {
		t.Fatalf("nil input: got %q, want empty", got)
	}
	if got := authTokenFromInput(map[string]any{}); got != "" {
		t.Fatalf("empty input: got %q, want empty", got)
	}
	if got := authTokenFromInput(map[string]any{"other": "x"}); got != "" {
		t.Fatalf("no auth key: got %q, want empty", got)
	}
	if got := authTokenFromInput(map[string]any{AuthTokenInputKey: "flag-token"}); got != "flag-token" {
		t.Fatalf("flag token: got %q, want flag-token", got)
	}
	// Non-string value must be ignored (defensive).
	if got := authTokenFromInput(map[string]any{AuthTokenInputKey: 42}); got != "" {
		t.Fatalf("non-string value: got %q, want empty", got)
	}
}

// TestPinsServiceAuthTokenPrecedence verifies that the per-invocation flag
// override threaded through the input map takes precedence over the
// deps.GetAuthToken() config fallback when building the service.
func TestPinsServiceAuthTokenPrecedence(t *testing.T) {
	var gotToken string
	fake := PinsDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, token string) pinning.PinningService {
			gotToken = token
			return nil
		},
		GetAuthToken: func() string { return "config-token" },
		ServiceFactory: func(_ config.Manager, _ bool) pinning.PinningService {
			t.Fatal("ServiceFactory must not be used when an auth token override is present")
			return nil
		},
	}

	// Flag override wins over config fallback.
	if _, err := fake.service(map[string]any{AuthTokenInputKey: "flag-token"}); err != nil {
		t.Fatalf("service: unexpected error: %v", err)
	}
	if gotToken != "flag-token" {
		t.Fatalf("flag override: NewAuthenticated got %q, want flag-token", gotToken)
	}

	// No flag override -> config fallback via GetAuthToken.
	if _, err := fake.service(map[string]any{}); err != nil {
		t.Fatalf("service: unexpected error: %v", err)
	}
	if gotToken != "config-token" {
		t.Fatalf("config fallback: NewAuthenticated got %q, want config-token", gotToken)
	}
}

// TestPinsServiceNilConfigError verifies that a nil config manager (e.g. the
// CfgMgr getter failed) yields a clean error instead of a nil-pointer panic in
// NewAuthenticated / ServiceFactory.
func TestPinsServiceNilConfigError(t *testing.T) {
	d := PinsDeps{
		// CfgMgr unset -> config() returns nil -> service() must error.
		ServiceFactory: func(_ config.Manager, _ bool) pinning.PinningService {
			t.Fatal("ServiceFactory must not be reached when config manager is nil")
			return nil
		},
	}
	_, err := d.service(map[string]any{})
	if err == nil {
		t.Fatal("service: expected error for nil config manager, got nil")
	}
}

// TestPinsServiceExported mirrors the precedence behavior through the exported
// Service() accessor, which the CLI wiring uses to build a service outside a
// handler context (e.g. to enumerate pins for the unpin-all safety prompt).
func TestPinsServiceExported(t *testing.T) {
	var gotToken string
	d := PinsDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, token string) pinning.PinningService {
			gotToken = token
			return nil
		},
		GetAuthToken: func() string { return "config-token" },
	}
	if _, err := d.Service(map[string]any{AuthTokenInputKey: "flag-token"}); err != nil {
		t.Fatalf("Service: unexpected error: %v", err)
	}
	if gotToken != "flag-token" {
		t.Fatalf("Service flag override: got %q, want flag-token", gotToken)
	}
	if _, err := d.Service(map[string]any{}); err != nil {
		t.Fatalf("Service: unexpected error: %v", err)
	}
	if gotToken != "config-token" {
		t.Fatalf("Service config fallback: got %q, want config-token", gotToken)
	}
}
