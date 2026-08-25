package catalogops

import (
	"context"
	"errors"
	"testing"

	coreadmin "go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

// fakePlatformDomainService is a hand-rolled admin.PlatformDomainAdminService
// whose methods are driven by function fields, so tests can assert both the
// plumbing (RequireAuthenticated gating, argument forwarding) and the op
// result wrapping without mocks.
type fakePlatformDomainService struct {
	requireAuth   func() error
	listFn        func(ctx context.Context) ([]*admin.PlatformDomain, int, error)
	registerFn    func(ctx context.Context, req *admin.PlatformDomainRequest) (*admin.PlatformDomain, error)
	deleteFn      func(ctx context.Context, id string) error
	updateFn      func(ctx context.Context, id string, req *admin.PlatformDomainUpdateRequest) (*admin.PlatformDomain, error)
	bindFn        func(ctx context.Context, id string, req *admin.PlatformDomainBindRequest) (*admin.RootDomain, error)
}

func (f *fakePlatformDomainService) RequireAuthenticated() error {
	if f.requireAuth != nil {
		return f.requireAuth()
	}
	return nil
}
func (f *fakePlatformDomainService) ListPlatformDomains(ctx context.Context) ([]*admin.PlatformDomain, int, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, 0, nil
}
func (f *fakePlatformDomainService) RegisterPlatformDomain(ctx context.Context, req *admin.PlatformDomainRequest) (*admin.PlatformDomain, error) {
	if f.registerFn != nil {
		return f.registerFn(ctx, req)
	}
	return nil, nil
}
func (f *fakePlatformDomainService) DeletePlatformDomain(ctx context.Context, id string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}
func (f *fakePlatformDomainService) UpdatePlatformDomain(ctx context.Context, id string, req *admin.PlatformDomainUpdateRequest) (*admin.PlatformDomain, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, id, req)
	}
	return nil, nil
}
func (f *fakePlatformDomainService) BindWebsiteToPlatformDomain(ctx context.Context, id string, req *admin.PlatformDomainBindRequest) (*admin.RootDomain, error) {
	if f.bindFn != nil {
		return f.bindFn(ctx, id, req)
	}
	return nil, nil
}

// testAdminDeps wires a fake platform-domain service into an AdminDeps whose
// CfgMgr returns a fresh config mock. The service getter returns the fake
// directly without reading config, so no Config() expectation is needed.
func testAdminDeps(t *testing.T, svc *fakePlatformDomainService) AdminDeps {
	return AdminDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		PlatformDomainAdminService: func(cfgMgr config.Manager) (coreadmin.PlatformDomainAdminService, error) {
			return svc, nil
		},
	}
}

// fakeWebsiteService is a hand-rolled admin.WebsiteAdminService driven by
// function fields.
type fakeWebsiteService struct {
	requireAuth func() error
	blockFn     func(ctx context.Context, id string) (*admin.Website, error)
	unblockFn   func(ctx context.Context, id string) (*admin.Website, error)
}

func (f *fakeWebsiteService) RequireAuthenticated() error {
	if f.requireAuth != nil {
		return f.requireAuth()
	}
	return nil
}
func (f *fakeWebsiteService) BlockWebsite(ctx context.Context, id string) (*admin.Website, error) {
	if f.blockFn != nil {
		return f.blockFn(ctx, id)
	}
	return nil, nil
}
func (f *fakeWebsiteService) UnblockWebsite(ctx context.Context, id string) (*admin.Website, error) {
	if f.unblockFn != nil {
		return f.unblockFn(ctx, id)
	}
	return nil, nil
}

// testAdminWebsitesDeps wires a fake website service into an AdminDeps.
func testAdminWebsitesDeps(t *testing.T, svc *fakeWebsiteService) AdminDeps {
	return AdminDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		WebsiteAdminService: func(cfgMgr config.Manager) (coreadmin.WebsiteAdminService, error) {
			return svc, nil
		},
	}
}

// TestAdminOperationsReturnsPlatformDomains asserts the provider registers the
// platform-domain operations.
func TestAdminOperationsReturnsPlatformDomains(t *testing.T) {
	ops := AdminOperations(AdminDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"admin_platform_domains_list",
		"admin_platform_domains_register",
		"admin_platform_domains_update",
		"admin_platform_domains_delete",
		"admin_platform_domains_bind",
	} {
		if !names[want] {
			t.Fatalf("AdminOperations missing expected op %q", want)
		}
	}
}

// TestAdminPlatformDomainsListNilDeps asserts an unwired service getter
// degrades to a clear error rather than panicking.
func TestAdminPlatformDomainsListNilDeps(t *testing.T) {
	op := adminPlatformDomainsList(AdminDeps{})
	_, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected an error when the platform-domain service is not wired")
	}
}

// TestAdminPlatformDomainsList verifies gating + result wrapping.
func TestAdminPlatformDomainsList(t *testing.T) {
	svc := &fakePlatformDomainService{
		requireAuth: func() error { return nil },
		listFn: func(ctx context.Context) ([]*admin.PlatformDomain, int, error) {
			d := &admin.PlatformDomain{}
			d.Id = 1
			d.Domain = "ipfs.pin.xyz"
			d.Namespace = "icann"
			return []*admin.PlatformDomain{d}, 1, nil
		},
	}
	op := adminPlatformDomainsList(testAdminDeps(t, svc))
	res, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got, ok := res.(*AdminPlatformDomainsListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if got.Count != 1 || len(got.PlatformDomains) != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// TestAdminPlatformDomainsListAuthGate asserts RequireAuthenticated is honored.
func TestAdminPlatformDomainsListAuthGate(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminPlatformDomainsList(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}

// TestAdminPlatformDomainsRegisterAuthGate asserts the mutating register op
// honors the auth gate before invoking the service.
func TestAdminPlatformDomainsRegisterAuthGate(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminPlatformDomainsRegister(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"domain": "x.pin.xyz", "namespace": "icann"})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}

// TestAdminPlatformDomainsUpdateAuthGate asserts the mutating update op honors
// the auth gate.
func TestAdminPlatformDomainsUpdateAuthGate(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminPlatformDomainsUpdate(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "7", "enabled": true})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}

// TestAdminPlatformDomainsDeleteAuthGate asserts the destructive delete op
// honors the auth gate.
func TestAdminPlatformDomainsDeleteAuthGate(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminPlatformDomainsDelete(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "7", "confirm": true})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}

// TestAdminPlatformDomainsBindAuthGate asserts the mutating bind op honors the
// auth gate.
func TestAdminPlatformDomainsBindAuthGate(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminPlatformDomainsBind(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "7", "website-id": 3})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}

// TestAdminPlatformDomainsDeleteRequiresConfirm asserts the destructive op
// rejects without confirm=true.
func TestAdminPlatformDomainsDeleteRequiresConfirm(t *testing.T) {
	svc := &fakePlatformDomainService{requireAuth: func() error { return nil }}
	op := adminPlatformDomainsDelete(testAdminDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "7"})
	if err == nil {
		t.Fatal("expected confirm-required error")
	}
}

// TestAdminPlatformDomainsBindForwardsWebsiteID verifies argument forwarding to
// the core service.
func TestAdminPlatformDomainsBindForwardsWebsiteID(t *testing.T) {
	var gotID string
	var gotReq *admin.PlatformDomainBindRequest
	svc := &fakePlatformDomainService{
		requireAuth: func() error { return nil },
		bindFn: func(ctx context.Context, id string, req *admin.PlatformDomainBindRequest) (*admin.RootDomain, error) {
			gotID = id
			gotReq = req
			r := &admin.RootDomain{}
			r.Id = 9
			r.Domain = "pinner.site"
			return r, nil
		},
	}
	op := adminPlatformDomainsBind(testAdminDeps(t, svc))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"id": "3", "website-id": 42})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if gotID != "3" || gotReq == nil || gotReq.WebsiteId != 42 {
		t.Fatalf("bind forwarded wrong args: id=%q req=%+v", gotID, gotReq)
	}
	if root, ok := res.(*admin.RootDomain); !ok || root.Domain != "pinner.site" {
		t.Fatalf("unexpected bind result %T", res)
	}
}

// TestAdminPlatformDomainsRegisterForwardsEnabled verifies the optional enabled
// flag reaches the request: omitted leaves Enabled nil (backend default), an
// explicit true/false is forwarded.
func TestAdminPlatformDomainsRegisterForwardsEnabled(t *testing.T) {
	cases := []struct {
		name        string
		input       map[string]any
		wantNil     bool
		wantEnabled bool
	}{
		{"omitted leaves nil", map[string]any{"domain": "ipfs.pin.xyz", "namespace": "icann"}, true, false},
		{"true enables", map[string]any{"domain": "ipfs.pin.xyz", "namespace": "icann", "enabled": true}, false, true},
		{"false disables", map[string]any{"domain": "ipfs.pin.xyz", "namespace": "icann", "enabled": false}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq *admin.PlatformDomainRequest
			svc := &fakePlatformDomainService{
				requireAuth: func() error { return nil },
				registerFn: func(ctx context.Context, req *admin.PlatformDomainRequest) (*admin.PlatformDomain, error) {
					gotReq = req
					d := &admin.PlatformDomain{}
					d.Domain = req.Domain
					return d, nil
				},
			}
			op := adminPlatformDomainsRegister(testAdminDeps(t, svc))
			if _, err := op.Handler().Execute(context.Background(), tc.input); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if tc.wantNil {
				if gotReq.Enabled != nil {
					t.Fatalf("expected nil Enabled, got %v", *gotReq.Enabled)
				}
				return
			}
			if gotReq.Enabled == nil || *gotReq.Enabled != tc.wantEnabled {
				t.Fatalf("expected Enabled=%v, got %v", tc.wantEnabled, gotReq.Enabled)
			}
		})
	}
}

// TestAdminWebsitesBlockForwardsID verifies the website admin block op forwards
// the positional id and honors authentication.
func TestAdminWebsitesBlockForwardsID(t *testing.T) {
	var gotID string
	svc := &fakeWebsiteService{
		requireAuth: func() error { return nil },
		blockFn: func(ctx context.Context, id string) (*admin.Website, error) {
			gotID = id
			w := &admin.Website{}
			w.Domain = "blocked.example"
			w.Id = 7
			return w, nil
		},
	}
	op := adminWebsitesBlock(testAdminWebsitesDeps(t, svc))
	res, err := op.Handler().Execute(context.Background(), map[string]any{"id": "3"})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if gotID != "3" {
		t.Fatalf("block forwarded wrong id %q", gotID)
	}
	if w, ok := res.(*admin.Website); !ok || w.Domain != "blocked.example" {
		t.Fatalf("unexpected block result %T", res)
	}
}

// TestAdminWebsitesUnblockForwardsID verifies the unblock op forwards the id.
func TestAdminWebsitesUnblockForwardsID(t *testing.T) {
	var gotID string
	svc := &fakeWebsiteService{
		requireAuth: func() error { return nil },
		unblockFn: func(ctx context.Context, id string) (*admin.Website, error) {
			gotID = id
			w := &admin.Website{}
			w.Domain = "ok.example"
			return w, nil
		},
	}
	op := adminWebsitesUnblock(testAdminWebsitesDeps(t, svc))
	if _, err := op.Handler().Execute(context.Background(), map[string]any{"id": "9"}); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if gotID != "9" {
		t.Fatalf("unblock forwarded wrong id %q", gotID)
	}
}

// TestAdminWebsitesUnblockAuthGate asserts the unblock op honors the auth gate
// and does not invoke the service when unauthenticated.
func TestAdminWebsitesUnblockAuthGate(t *testing.T) {
	called := false
	svc := &fakeWebsiteService{
		requireAuth: func() error { return errors.New("not authenticated") },
		unblockFn: func(ctx context.Context, id string) (*admin.Website, error) {
			called = true
			return nil, nil
		},
	}
	op := adminWebsitesUnblock(testAdminWebsitesDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "1"})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
	if called {
		t.Fatal("unblock service was invoked despite failed auth gate")
	}
}

// TestAdminWebsitesAuthGate asserts RequireAuthenticated is honored.
func TestAdminWebsitesAuthGate(t *testing.T) {
	svc := &fakeWebsiteService{requireAuth: func() error { return errors.New("not authenticated") }}
	op := adminWebsitesBlock(testAdminWebsitesDeps(t, svc))
	_, err := op.Handler().Execute(context.Background(), map[string]any{"id": "1"})
	if err == nil || err.Error() != "not authenticated" {
		t.Fatalf("expected not-authenticated error, got %v", err)
	}
}
