package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	coreadmin "go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

// TestAdminPlatformDomainsTree asserts the platform-domains command compiled
// from the operation catalog exposes the five subcommands (list, register,
// update, delete, bind), matching the MCP admin_platform_domains_* tools.
func TestAdminPlatformDomainsTree(t *testing.T) {
	cmd := newAdminPlatformDomainsCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "platform-domains", cmd.Name)

	names := getSubcommandNames(cmd)
	for _, want := range []string{"list", "register", "update", "delete", "bind"} {
		assert.Contains(t, names, want)
	}
}

// captureHandler is a catalog.Handler stub that records invocation and returns
// a routable admin result, letting the tests exercise adminActionAdapter's
// input mapping and destructive gate without a real admin service.
type captureHandler struct {
	called *bool
	input  map[string]any
}

func (h *captureHandler) Execute(_ context.Context, input map[string]any) (any, error) {
	*h.called = true
	h.input = input
	return &catalogops.AdminPlatformDomainsDeleteResult{Deleted: true, ID: "7"}, nil
}

// deleteOp builds a minimal catalog delete-style operation for testing the
// CLI adapter's destructive confirm gate.
func deleteOp(called *bool) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:       "admin_platform_domains_delete",
		Safety:     catalog.SafetyDestructive,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, PositionalOnly: true},
			{Name: "confirm", Type: catalog.ArgTypeBool, AgentRequired: true, Default: "true"},
		},
		Handler: &captureHandler{called: called},
	})
}

// TestAdminActionAdapterDeleteRunsWithoutForce asserts the admin platform-domains
// delete command no longer requires a --force toggle: an explicit CLI delete is
// an authoritative human action, so the handler runs and confirm is set true.
func TestAdminActionAdapterDeleteRunsWithoutForce(t *testing.T) {
	deleted := false
	op := deleteOp(&deleted)
	cmd := &cli.Command{
		Name:   "delete",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: FlagForce}, &cli.BoolFlag{Name: FlagConfirm}},
		Action: adminActionAdapter(op),
	}

	// Delete without --force proceeds and invokes the handler with confirm=true.
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "7"}))
	require.True(t, deleted, "delete handler must be invoked without --force")
	h := op.Handler().(*captureHandler)
	assert.Equal(t, true, h.input["confirm"], "delete must set confirm=true for a human CLI action")
}

// TestAdminActionAdapterForwardsPositionalAndFlags verifies the adapter maps a
// positional id and flag values into the handler input on the way through.
func TestAdminActionAdapterForwardsPositionalAndFlags(t *testing.T) {
	deleted := false
	op := deleteOp(&deleted)
	cmd := &cli.Command{
		Name:   "delete",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: FlagForce}, &cli.BoolFlag{Name: FlagConfirm}},
		Action: adminActionAdapter(op),
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "--force", "42"}))
	require.True(t, deleted)
	h := op.Handler().(*captureHandler)
	assert.Equal(t, "42", h.input["id"])
	assert.Equal(t, true, h.input["confirm"])
}

// fakePlatformDomainService is a hand-rolled admin.PlatformDomainAdminService
// driven by function fields, used to exercise resolvePlatformDomainID without a
// real service or network.
type fakePlatformDomainService struct {
	listFn func(ctx context.Context) ([]*admin.PlatformDomain, int, error)
}

func (f *fakePlatformDomainService) RequireAuthenticated() error { return nil }
func (f *fakePlatformDomainService) ListPlatformDomains(ctx context.Context) ([]*admin.PlatformDomain, int, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, 0, nil
}
func (f *fakePlatformDomainService) RegisterPlatformDomain(ctx context.Context, _ *admin.PlatformDomainRequest) (*admin.PlatformDomain, error) {
	return nil, nil
}
func (f *fakePlatformDomainService) DeletePlatformDomain(ctx context.Context, _ string) error {
	return nil
}
func (f *fakePlatformDomainService) UpdatePlatformDomain(ctx context.Context, _ string, _ *admin.PlatformDomainUpdateRequest) (*admin.PlatformDomain, error) {
	return nil, nil
}
func (f *fakePlatformDomainService) BindWebsiteToPlatformDomain(ctx context.Context, _ string, _ *admin.PlatformDomainBindRequest) (*admin.RootDomain, error) {
	return nil, nil
}

// TestResolvePlatformDomainID asserts resolvePlatformDomainID passes numeric ids
// through unchanged and resolves a registered domain name to its numeric id via
// ListPlatformDomains.
func TestResolvePlatformDomainID(t *testing.T) {
	svc := &fakePlatformDomainService{
		listFn: func(ctx context.Context) ([]*admin.PlatformDomain, int, error) {
			d1 := &admin.PlatformDomain{}
			d1.Id, d1.Domain = 7, "pinned.site"
			d2 := &admin.PlatformDomain{}
			d2.Id, d2.Domain = 9, "example.com"
			return []*admin.PlatformDomain{d1, d2}, 2, nil
		},
	}
	deps := catalogops.AdminDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		PlatformDomainAdminService: func(cfgMgr config.Manager) (coreadmin.PlatformDomainAdminService, error) {
			return svc, nil
		},
	}

	got, err := resolvePlatformDomainID(context.Background(), deps, "42")
	require.NoError(t, err)
	assert.Equal(t, "42", got)

	got, err = resolvePlatformDomainID(context.Background(), deps, "pinned.site")
	require.NoError(t, err)
	assert.Equal(t, "7", got)

	_, err = resolvePlatformDomainID(context.Background(), deps, "nope.test")
	require.Error(t, err)
}
