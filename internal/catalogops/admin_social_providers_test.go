package catalogops

import (
	"context"
	"strings"
	"testing"

	coreadmin "go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

// fakeSocialProviderService is a hand-rolled admin.SocialProviderAdminService
// whose methods are driven by function fields, so tests can assert both the
// plumbing (RequireAuthenticated gating, argument forwarding) and the op
// result wrapping without mocks.
type fakeSocialProviderService struct {
	requireAuth func() error
	listFn      func(ctx context.Context) ([]*admin.SocialProvider, int, error)
	getFn       func(ctx context.Context, id string) (*admin.SocialProvider, error)
	createFn    func(ctx context.Context, req *admin.SocialProviderRequest) (*admin.SocialProvider, error)
	updateFn    func(ctx context.Context, id string, req *admin.SocialProviderRequest) (*admin.SocialProvider, error)
	deleteFn    func(ctx context.Context, id string) error
	enableFn    func(ctx context.Context, id string) (*admin.SocialProvider, error)
	disableFn   func(ctx context.Context, id string) (*admin.SocialProvider, error)
}

func (f *fakeSocialProviderService) RequireAuthenticated() error {
	if f.requireAuth != nil {
		return f.requireAuth()
	}
	return nil
}

func (f *fakeSocialProviderService) ListSocialProviders(ctx context.Context) ([]*admin.SocialProvider, int, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, 0, nil
}

func (f *fakeSocialProviderService) GetSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return nil, nil
}

func (f *fakeSocialProviderService) CreateSocialProvider(ctx context.Context, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
	if f.createFn != nil {
		return f.createFn(ctx, req)
	}
	return nil, nil
}

func (f *fakeSocialProviderService) UpdateSocialProvider(ctx context.Context, id string, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, id, req)
	}
	return nil, nil
}

func (f *fakeSocialProviderService) DeleteSocialProvider(ctx context.Context, id string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}

func (f *fakeSocialProviderService) EnableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	if f.enableFn != nil {
		return f.enableFn(ctx, id)
	}
	return nil, nil
}

func (f *fakeSocialProviderService) DisableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	if f.disableFn != nil {
		return f.disableFn(ctx, id)
	}
	return nil, nil
}

// testSocialProvidersDeps wires a fake social provider service into an
// AdminDeps whose CfgMgr returns a fresh config mock.
func testSocialProvidersDeps(t *testing.T, svc *fakeSocialProviderService) AdminDeps {
	return AdminDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		SocialProviderAdminService: func(cfgMgr config.Manager) (coreadmin.SocialProviderAdminService, error) {
			return svc, nil
		},
	}
}

// sampleSocialProvider returns a provider with a few fields filled in.
func sampleSocialProvider() *admin.SocialProvider {
	p := &admin.SocialProvider{}
	p.Id = 3
	p.ProviderId = "github"
	p.DisplayName = "GitHub"
	p.Enabled = true
	p.OrderIndex = 2
	p.Scopes = []string{"read:user", "user:email"}
	return p
}

// TestAdminOperationsReturnsSocialProviders asserts the provider registers the
// social provider operations.
func TestAdminOperationsReturnsSocialProviders(t *testing.T) {
	ops := AdminOperations(AdminDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"admin_social_providers_list",
		"admin_social_providers_get",
		"admin_social_providers_create",
		"admin_social_providers_update",
		"admin_social_providers_delete",
		"admin_social_providers_enable",
		"admin_social_providers_disable",
	} {
		if !names[want] {
			t.Fatalf("AdminOperations missing expected op %q", want)
		}
	}
}

// TestAdminSocialProvidersListNilDeps asserts an unwired service getter
// degrades to a clear error rather than panicking.
func TestAdminSocialProvidersListNilDeps(t *testing.T) {
	op := adminSocialProvidersList(AdminDeps{})
	_, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected an error when the social provider service is not wired")
	}
}

// TestAdminSocialProvidersList verifies gating + result wrapping.
func TestAdminSocialProvidersList(t *testing.T) {
	svc := &fakeSocialProviderService{
		requireAuth: func() error { return nil },
		listFn: func(ctx context.Context) ([]*admin.SocialProvider, int, error) {
			return []*admin.SocialProvider{sampleSocialProvider()}, 1, nil
		},
	}
	op := adminSocialProvidersList(testSocialProvidersDeps(t, svc))
	res, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got, ok := res.(ListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if got.ListCount() != 1 {
		t.Fatalf("unexpected count: %d", got.ListCount())
	}
	items, ok := got.ListItems().([]*admin.SocialProvider)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected result: %+v", got.ListItems())
	}
}

// TestAdminSocialProvidersListAuthGate asserts RequireAuthenticated is honored.
func TestAdminSocialProvidersListAuthGate(t *testing.T) {
	svc := &fakeSocialProviderService{requireAuth: func() error { return context.Canceled }}
	op := adminSocialProvidersList(testSocialProvidersDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected auth gate to reject the call")
	}
}

// TestAdminSocialProvidersCreateForwarding verifies required-field validation
// and that the request reaches the service untouched.
func TestAdminSocialProvidersCreateForwarding(t *testing.T) {
	var gotReq *admin.SocialProviderRequest
	svc := &fakeSocialProviderService{
		requireAuth: func() error { return nil },
		createFn: func(ctx context.Context, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
			gotReq = req
			return sampleSocialProvider(), nil
		},
	}
	op := adminSocialProvidersCreate(testSocialProvidersDeps(t,svc))
	input := map[string]any{
		"provider-id":    "github",
		"client-id":      "cid",
		"client-secret":  "shh",
		"display-name":   "GitHub",
		"auth-url":       "https://github.com/login/oauth/authorize",
		"token-url":      "https://github.com/login/oauth/access_token",
		"user-url":       "https://api.github.com/user",
		"scopes":         []string{"read:user"},
		"user-id-key":    "id",
		"user-email-key": "email",
		"user-name-key":  "name",
		"order-index":    2,
		"enabled":        true,
	}
	_, err := op.Handler().Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotReq == nil {
		t.Fatal("create request did not reach the service")
	}
	if gotReq.ProviderId != "github" || gotReq.ClientSecret != "shh" || gotReq.OrderIndex != 2 || !gotReq.Enabled {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
}

// TestAdminSocialProvidersCreateValidation asserts the required-field errors
// fire before the service is called.
func TestAdminSocialProvidersCreateValidation(t *testing.T) {
	cases := []struct {
		key, missing string
	}{
		{"provider-id", "provider-id"},
		{"client-id", "client-id"},
		{"client-secret", "client-secret"},
		{"display-name", "display-name"},
	}
	for _, tc := range cases {
		op := adminSocialProvidersCreate(testSocialProvidersDeps(t, &fakeSocialProviderService{}))
		input := map[string]any{
			"provider-id":   "github",
			"client-id":     "cid",
			"client-secret": "shh",
			"display-name":  "GitHub",
		}
		delete(input, tc.key)
		_, err := op.Handler().Execute(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), tc.missing) {
			t.Fatalf("expected error mentioning %q, got %v", tc.missing, err)
		}
	}
}

// TestAdminSocialProvidersUpdateMergesExisting asserts update re-supplies the
// client secret, merges omitted fields from the provider returned by get, and
// forwards the applied overrides.
func TestAdminSocialProvidersUpdateMergesExisting(t *testing.T) {
	var gotReq *admin.SocialProviderRequest
	var gotID string
	svc := &fakeSocialProviderService{
		requireAuth: func() error { return nil },
		getFn: func(ctx context.Context, id string) (*admin.SocialProvider, error) {
			return sampleSocialProvider(), nil
		},
		updateFn: func(ctx context.Context, id string, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
			gotID, gotReq = id, req
			return sampleSocialProvider(), nil
		},
	}
	op := adminSocialProvidersUpdate(testSocialProvidersDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"id":            "3",
		"client-secret": "new-secret",
		"display-name":  "GitHub (updated)",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if gotID != "3" {
		t.Fatalf("unexpected id %q", gotID)
	}
	if gotReq == nil {
		t.Fatal("update request did not reach the service")
	}
	if gotReq.ClientSecret != "new-secret" {
		t.Fatalf("supplied secret was not used: %+v", gotReq)
	}
	if gotReq.DisplayName != "GitHub (updated)" {
		t.Fatalf("override not applied: %+v", gotReq)
	}
	// Omitted fields must be carried over from the existing provider.
	if gotReq.ProviderId != "github" || len(gotReq.Scopes) != 2 || gotReq.OrderIndex != 2 || !gotReq.Enabled {
		t.Fatalf("existing fields not preserved: %+v", gotReq)
	}
}

// TestAdminSocialProvidersUpdateRequiresSecret asserts the full-replace contract
// for the client secret.
func TestAdminSocialProvidersUpdateRequiresSecret(t *testing.T) {
	called := false
	svc := &fakeSocialProviderService{
		updateFn: func(ctx context.Context, id string, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
			called = true
			return nil, nil
		},
	}
	op := adminSocialProvidersUpdate(testSocialProvidersDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "3"})
	if err == nil || !strings.Contains(err.Error(), "client-secret") {
		t.Fatalf("expected client-secret validation error, got %v", err)
	}
	if called {
		t.Fatal("service must not be called without the secret")
	}
}

// TestAdminSocialProvidersDelete asserts the confirm gate and result shape.
func TestAdminSocialProvidersDelete(t *testing.T) {
	var gotID string
	svc := &fakeSocialProviderService{
		deleteFn: func(ctx context.Context, id string) error {
			gotID = id
			return nil
		},
	}
	op := adminSocialProvidersDelete(testSocialProvidersDeps(t, svc))

	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "3"})
	if err == nil || !strings.Contains(err.Error(), "confirmation is required") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	res, err := op.Handler().Execute(context.Background(), map[string]any{"id": "3", "confirm": true})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gotID != "3" {
		t.Fatalf("unexpected id %q", gotID)
	}
	dr, ok := res.(*SocialProvidersDeleteResult)
	if !ok || !dr.Deleted || dr.ID != "3" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestAdminSocialProvidersEnableDisableForwarding verifies the id reaches the
// service and the SDK provider result passes through.
func TestAdminSocialProvidersEnableDisableForwarding(t *testing.T) {
	var enableID, disableID string
	svc := &fakeSocialProviderService{
		enableFn: func(ctx context.Context, id string) (*admin.SocialProvider, error) {
			enableID = id
			return sampleSocialProvider(), nil
		},
		disableFn: func(ctx context.Context, id string) (*admin.SocialProvider, error) {
			disableID = id
			return sampleSocialProvider(), nil
		},
	}
	opts := testSocialProvidersDeps(t, svc)

	en, err := adminSocialProvidersEnable(opts).Handler().Execute(context.Background(), map[string]any{"id": "3"})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if enableID != "3" {
		t.Fatalf("enable id %q", enableID)
	}
	if _, ok := en.(*admin.SocialProvider); !ok {
		t.Fatalf("unexpected enable result %T", en)
	}

	if _, err := adminSocialProvidersDisable(opts).Handler().Execute(context.Background(), map[string]any{"id": "4"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disableID != "4" {
		t.Fatalf("disable id %q", disableID)
	}
}
