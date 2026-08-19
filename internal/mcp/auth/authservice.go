package auth

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// AuthService is the subset of core/auth.AuthService used by the MCP wizard
// and the out-of-band account credential flows.
type AuthService interface {
	LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error)
	CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error)
	LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error)
	Status(ctx context.Context) (*auth.StatusResult, error)
	// UpdatePassword changes the account password given the current password.
	// Used by the out-of-band password change page, always from the
	// authenticated session, so the password never transits the MCP/LLM channel.
	UpdatePassword(ctx context.Context, currentPassword, newPassword string) error
	// UpdateEmail changes the account email given the current password for
	// verification. Used by the out-of-band email change page, always from the
	// authenticated session, so the password never transits the MCP/LLM channel.
	UpdateEmail(ctx context.Context, email, currentPassword string) error
	// RequestPasswordReset sends a password reset link to the account's email
	// (unauthenticated, so it works for a forgotten password). The human
	// completes the reset via the emailed link in the web app.
	RequestPasswordReset(ctx context.Context, email string) error
}
