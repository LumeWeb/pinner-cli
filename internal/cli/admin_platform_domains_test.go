package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
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

// TestAdminActionAdapterDeleteRequiresForce verifies the destructive --force
// gate: delete without --force/--confirm is rejected before the handler runs;
// with --force the handler is invoked.
func TestAdminActionAdapterDeleteRequiresForce(t *testing.T) {
	deleted := false
	op := deleteOp(&deleted)
	cmd := &cli.Command{
		Name:    "delete",
		Flags:   []cli.Flag{&cli.BoolFlag{Name: FlagForce}, &cli.BoolFlag{Name: FlagConfirm}},
		Action:  adminActionAdapter(op),
	}

	// No --force: rejected, handler never invoked. (Run drops os.Args[0], so a
	// placeholder program name is supplied.)
	err := cmd.Run(context.Background(), []string{"pinner", "7"})
	require.Error(t, err)
	require.False(t, deleted, "delete handler must not be invoked without --force")

	// With --force: proceeds and invokes the handler.
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "--force", "7"}))
	require.True(t, deleted, "delete handler must be invoked with --force")
}

// TestAdminActionAdapterForwardsPositionalAndFlags verifies the adapter maps a
// positional id and flag values into the handler input on the way through.
func TestAdminActionAdapterForwardsPositionalAndFlags(t *testing.T) {
	deleted := false
	op := deleteOp(&deleted)
	cmd := &cli.Command{
		Name:    "delete",
		Flags:   []cli.Flag{&cli.BoolFlag{Name: FlagForce}, &cli.BoolFlag{Name: FlagConfirm}},
		Action:  adminActionAdapter(op),
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "--force", "42"}))
	require.True(t, deleted)
	h := op.Handler().(*captureHandler)
	assert.Equal(t, "42", h.input["id"])
	assert.Equal(t, true, h.input["confirm"])
}
