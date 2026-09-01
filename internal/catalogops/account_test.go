package catalogops

import (
	"context"
	"errors"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	account "go.lumeweb.com/portal-sdk"
)

// fakeAuthService mocks auth.AuthService for the account otp disable handler,
// overriding only the methods each test exercises. It embeds the real
// interface so unimplemented methods panic rather than silently succeed.
type fakeAuthService struct {
	auth.AuthService

	disableOTPFn func(ctx context.Context, password string) (*auth.DisableOTPResult, error)
	quotaFn      func(ctx context.Context) (*account.QuotaStatus, error)
}

func (f *fakeAuthService) DisableOTP(ctx context.Context, password string) (*auth.DisableOTPResult, error) {
	if f.disableOTPFn != nil {
		return f.disableOTPFn(ctx, password)
	}
	return &auth.DisableOTPResult{}, nil
}

func (f *fakeAuthService) GetQuota(ctx context.Context) (*account.QuotaStatus, error) {
	if f.quotaFn != nil {
		return f.quotaFn(ctx)
	}
	return &account.QuotaStatus{}, nil
}

func intPtr(v int) *int { return &v }

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

// TestAccountUpdateEnvCLIOnly verifies account_update_email and
// account_update_password declare EnvCLIOnly: they are valid only on the urfave
// CLI frontend and are omitted from every MCP surface (they pass the user's
// password through the LLM channel and duplicate the OOB browser hand-off
// tools).
func TestAccountUpdateEnvCLIOnly(t *testing.T) {
	envs := map[string]catalog.Environment{}
	for _, op := range AccountOperations(AccountDeps{}) {
		envs[op.Name()] = op.Environment()
	}
	for _, name := range []string{"account_update_email", "account_update_password"} {
		if envs[name] != catalog.EnvCLIOnly {
			t.Errorf("%s.Environment() = %v, want EnvCLIOnly", name, envs[name])
		}
	}
	if envs["account_info"] != catalog.EnvBoth {
		t.Errorf("account_info.Environment() = %v, want EnvBoth", envs["account_info"])
	}
}

// TestAccountQuotaReturnsTypedResult verifies the account_quota handler maps a
// positive-remaining quota into a typed result with has_quota=true (quota
// covers access regardless of subscription).
func TestAccountQuotaReturnsTypedResult(t *testing.T) {
	fake := &fakeAuthService{
		quotaFn: func(_ context.Context) (*account.QuotaStatus, error) {
			q := &account.QuotaStatus{}
			q.Upload.Used = 5
			q.Upload.Limit = intPtr(100)
			q.Upload.Remaining = intPtr(95)
			q.Upload.Percentage = 5
			q.Download.Remaining = intPtr(0)
			return q, nil
		},
	}
	op := accountQuota(accountDisableDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("account quota handler: %v", err)
	}
	result, ok := res.(*AccountQuotaResult)
	if !ok {
		t.Fatalf("result type = %T, want *AccountQuotaResult", res)
	}
	if !result.HasQuota {
		t.Fatal("expected has_quota=true when upload has remaining allowance")
	}
	if result.Upload.Used != 5 || result.Upload.Remaining == nil || *result.Upload.Remaining != 95 {
		t.Fatalf("upload fields wrong: %+v", result.Upload)
	}
	if result.Message == "" {
		t.Fatal("expected a human-readable message")
	}
}

// TestAccountQuotaNoRemaining verifies has_quota=false when every dimension is
// exhausted, and the message reflects that a subscription/grant is needed.
func TestAccountQuotaNoRemaining(t *testing.T) {
	fake := &fakeAuthService{
		quotaFn: func(_ context.Context) (*account.QuotaStatus, error) {
			q := &account.QuotaStatus{}
			q.Upload.Remaining = intPtr(0)
			q.Download.Remaining = intPtr(0)
			q.Storage.Remaining = intPtr(0)
			return q, nil
		},
	}
	op := accountQuota(accountDisableDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("account quota handler: %v", err)
	}
	result := res.(*AccountQuotaResult)
	if result.HasQuota {
		t.Fatal("expected has_quota=false when all dimensions are exhausted")
	}
}

// TestAccountQuotaUnlimitedIsCovered verifies a dimension with no remaining
// bound (unlimited) counts as covered quota.
func TestAccountQuotaUnlimitedIsCovered(t *testing.T) {
	fake := &fakeAuthService{
		quotaFn: func(_ context.Context) (*account.QuotaStatus, error) {
			return &account.QuotaStatus{}, nil // all remaining==nil => unlimited
		},
	}
	op := accountQuota(accountDisableDeps(t, fake))
	res, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("account quota handler: %v", err)
	}
	result := res.(*AccountQuotaResult)
	if !result.HasQuota {
		t.Fatal("expected has_quota=true when quota is unlimited")
	}
}

// TestAccountQuotaPropagatesServiceError verifies the handler propagates a
// GetQuota error with the operation prefix.
func TestAccountQuotaPropagatesServiceError(t *testing.T) {
	fake := &fakeAuthService{
		quotaFn: func(_ context.Context) (*account.QuotaStatus, error) {
			return nil, errors.New("quota service down")
		},
	}
	op := accountQuota(accountDisableDeps(t, fake))
	_, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when GetQuota fails")
	}
	if err.Error() != "account_quota: quota service down" {
		t.Fatalf("err = %q, want account_quota: quota service down", err.Error())
	}
}
