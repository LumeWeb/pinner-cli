package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/opmesh"

	"go.lumeweb.com/pinner/catalogops"
)

// Verify the 8 workspaces_* lifecycle ops are registered in
// WorkspacesOperations (the source AssembleCatalogOps feeds from for the MCP
// surface), that runtime resolve is deliberately absent, and that the
// sensitive-credential op is gated to humans.
func TestWorkspacesOpsRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.WorkspacesOperations(catalogops.WorkspacesDeps{})
	byName := map[string]opmesh.Operation{}
	for _, op := range ops {
		byName[op.Name()] = op
	}
	for _, want := range []string{
		"workspaces_list",
		"workspaces_create",
		"workspaces_get",
		"workspaces_attach",
		"workspaces_suspend",
		"workspaces_resume",
		"workspaces_access",
		"workspaces_delete",
	} {
		require.True(t, byName[want] != nil, "MCP surface should expose %s", want)
	}
	_, resolve := byName["workspaces_resolve"]
	assert.False(t, resolve, "runtime workspace resolve must not surface as an MCP operation")

	// The creds op is discoverable (visibility both) but human-gated.
	access, ok := byName["workspaces_access"]
	require.True(t, ok)
	assert.Equal(t, opmesh.InteractionHumanOnly, access.Interaction())
	assert.Equal(t, opmesh.VisibilityBoth, access.Visibility())
}
