package mcp

// Shared test helpers for auth-domain app-view tests. These live in the hub
// package (root) because the app-view registration tests exercise the catalog
// and AppView wiring here, while the auth core itself is tested in auth/.
// Each package keeps its own stub so neither crosses the package boundary for
// test-only symbols.

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"

	authcore "go.lumeweb.com/pinner-cli/internal/core/auth"
	mcpauth "go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// stubAuthService is a minimal AuthService double for hub-side tests that wire
// the auth descriptors and apps the way a production client would. It is the
// root-package counterpart of the auth/ package's own stub.
type stubAuthService struct{}

func (s stubAuthService) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	return &portalsdk.LoginResult{Token: "test-token", OTPRequired: false}, nil
}
func (s stubAuthService) CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) (*authcore.LoginCompleteResult, error) {
	return nil, nil
}
func (s stubAuthService) LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*authcore.LoginCompleteResult, error) {
	return nil, nil
}
func (s stubAuthService) Status(ctx context.Context) (*authcore.StatusResult, error) { return nil, nil }
func (s stubAuthService) UpdatePassword(ctx context.Context, currentPassword, newPassword string) error {
	return nil
}
func (s stubAuthService) UpdateEmail(ctx context.Context, email, currentPassword string) error {
	return nil
}
func (s stubAuthService) RequestPasswordReset(ctx context.Context, email string) error { return nil }

// newHubOOBForTest builds an OutOfBandLogin with the hub-side stub so app-view
// tests can drive the Sign In flow through the catalog layer.
func newHubOOBForTest(t *testing.T) *mcpauth.OutOfBandLogin {
	t.Helper()
	o := mcpauth.NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	return o
}

// requireHandoff asserts a tool result is a needs_human hand-off and returns its
// structured content, so root-side vault/app-view tests can assert the hand-off
// shape without depending on the auth package's own test helpers.
func requireHandoff(t *testing.T, r model.ToolResult) map[string]any {
	t.Helper()
	require.False(t, r.IsError, "expected needs_human hand-off, got error: %s", r.Text)
	sc, ok := r.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	return sc
}
