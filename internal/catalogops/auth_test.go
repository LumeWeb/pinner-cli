package catalogops

import (
	"context"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
	portalsdk "go.lumeweb.com/portal-sdk"
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

// statusCapturingAuthService records the token it was constructed with and
// returns a minimal authenticated Status so auth_status reports success. It
// embeds the interface so unimplemented methods panic rather than silently
// succeed.
type statusCapturingAuthService struct {
	auth.AuthService
	gotToken string
}

func (s *statusCapturingAuthService) Status(ctx context.Context) (*auth.StatusResult, error) {
	return &auth.StatusResult{PortalURL: "https://portal"}, nil
}

func (s *statusCapturingAuthService) GetAccount(ctx context.Context) (*portalsdk.AccountInfo, error) {
	return nil, nil
}

// TestAuthStatusThreadsPerRequestToken verifies auth_status passes the
// per-invocation --auth-token override (threaded by a hosted server's
// per-request CredentialResolver) into AuthService construction instead of
// running Status against the config-stored credential. On a hosted server the
// config token is empty, so this is what makes auth_status reflect the calling
// user rather than always reporting "Not authenticated".
func TestAuthStatusThreadsPerRequestToken(t *testing.T) {
	svc := &statusCapturingAuthService{}
	d := AuthDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		AuthService: func(_ config.Manager, token string) auth.AuthService {
			svc.gotToken = token
			return svc
		},
		ResolveAuthToken: func(_ config.Manager) string { return "config-token" },
	}

	// Flag override wins; the service must see it, not the config fallback.
	op := authStatus(d)
	res, err := op.Handler().Execute(context.Background(), map[string]any{AuthTokenInputKey: "flag-token"})
	if err != nil {
		t.Fatalf("auth_status handler: %v", err)
	}
	if svc.gotToken != "flag-token" {
		t.Fatalf("auth_status used token %q, want the per-request override %q", svc.gotToken, "flag-token")
	}
	st, ok := res.(*AuthStatusResult)
	if !ok {
		t.Fatalf("auth_status result type = %T, want *AuthStatusResult", res)
	}
	if !st.Authenticated {
		t.Fatal("auth_status reported unauthenticated despite a valid per-request token")
	}

	// No override -> the config fallback is used.
	svc.gotToken = ""
	if _, err := op.Handler().Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("auth_status handler (config path): %v", err)
	}
	if svc.gotToken != "config-token" {
		t.Fatalf("auth_status config fallback token = %q, want %q", svc.gotToken, "config-token")
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
