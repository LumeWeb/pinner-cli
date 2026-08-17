package catalogops

import (
	"context"
	"errors"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

// fakeAuthService mocks auth.AuthService for the account otp disable handler,
// overriding only the methods each test exercises. It embeds the real
// interface so unimplemented methods panic rather than silently succeed.
type fakeAuthService struct {
	auth.AuthService

	disableOTPFn func(ctx context.Context, password string) (*auth.DisableOTPResult, error)
}

func (f *fakeAuthService) DisableOTP(ctx context.Context, password string) (*auth.DisableOTPResult, error) {
	if f.disableOTPFn != nil {
		return f.disableOTPFn(ctx, password)
	}
	return &auth.DisableOTPResult{}, nil
}

// accountDisableDeps returns an AccountDeps whose auth service is backed by the
// given fake, with a config manager present so authClientHandler resolves.
func accountDisableDeps(t *testing.T, fake *fakeAuthService) AccountDeps {
	t.Helper()
	return AccountDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
			return fake
		},
	}
}

func TestAccountOTPDisableCallsDisableOTP(t *testing.T) {
	var gotPassword string
	fake := &fakeAuthService{
		disableOTPFn: func(_ context.Context, password string) (*auth.DisableOTPResult, error) {
			gotPassword = password
			return &auth.DisableOTPResult{}, nil
		},
	}

	const testPassword = "test-password-not-a-secret"
	op := accountOTPDisable(accountDisableDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"password": testPassword})
	if err != nil {
		t.Fatalf("otp disable handler: %v", err)
	}
	if gotPassword != testPassword {
		t.Fatalf("DisableOTP password = %q, want %q", gotPassword, testPassword)
	}
	result, ok := res.(*AccountOTPDisableResult)
	if !ok {
		t.Fatalf("result type = %T, want *AccountOTPDisableResult", res)
	}
	if result.Message != "Two-factor authentication disabled." {
		t.Fatalf("message = %q, want %q", result.Message, "Two-factor authentication disabled.")
	}
}

func TestAccountOTPDisableRequiresPassword(t *testing.T) {
	fake := &fakeAuthService{
		disableOTPFn: func(_ context.Context, password string) (*auth.DisableOTPResult, error) {
			t.Fatal("DisableOTP must not be called when password is missing")
			return nil, nil
		},
	}

	op := accountOTPDisable(accountDisableDeps(t, fake))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected error when password arg is missing")
	}
}

func TestAccountOTPDisablePropagatesServiceError(t *testing.T) {
	fake := &fakeAuthService{
		disableOTPFn: func(_ context.Context, password string) (*auth.DisableOTPResult, error) {
			return nil, errors.New("invalid password")
		},
	}

	op := accountOTPDisable(accountDisableDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"password": "wrong"})
	if err == nil {
		t.Fatal("expected error when DisableOTP fails")
	}
	if err.Error() != "account_otp_disable: invalid password" {
		t.Fatalf("err = %q, want account_otp_disable: invalid password", err.Error())
	}
}
