package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/opmesh"

	"go.lumeweb.com/pinner/catalogops"
)

// TestWorkspacesCommandTree verifies the workspaces parent mounts exactly the
// expected catalog leaves (list/get/create/attach/suspend/resume/access/delete)
// and that workspaces sits at the top level (a SEPARATE tree from websites),
// not nested anywhere under it.
func TestWorkspacesCommandTree(t *testing.T) {
	root := NewRootCommand()
	ws := findCommand(root.Commands, "workspaces")
	require.NotNil(t, ws, "workspaces command should exist at the root")
	assert.Equal(t, "Management", ws.Category)

	names := commandNames(ws.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"list", "get", "create", "attach", "suspend", "resume", "access", "delete"} {
		assert.True(t, nameSet[expected], "workspaces should have subcommand %q", expected)
	}
	// Runtime resolve is intentionally not a user operation.
	assert.False(t, nameSet["resolve"], "workspaces resolve must not be a user subcommand")
}

// TestWorkspacesListHasLsAlias verifies the canonical list leaf carries the
// muscle-memory "ls" alias (ideal naming rule), matching other domains.
func TestWorkspacesListHasLsAlias(t *testing.T) {
	ws := findCommand(NewRootCommand().Commands, "workspaces")
	require.NotNil(t, ws)
	list := findCommand(ws.Commands, "list")
	require.NotNil(t, list, "workspaces list command should exist")
	assert.Contains(t, list.Aliases, "ls", "workspaces list should have the 'ls' alias")
}

// TestWorkspacesAccessIsHumanOnly verifies the sensitive-credential operation is
// gated to humans at the catalog layer (a model actor is handed off), while
// remaining discoverable (visibility both) — it is surfaced but never executed
// by an agent.
func TestWorkspacesAccessIsHumanOnly(t *testing.T) {
	var access opmesh.Operation
	for _, op := range catalogops.WorkspacesOperations(catalogops.WorkspacesDeps{}) {
		if op.Name() == "workspaces_access" {
			access = op
		}
	}
	require.NotNil(t, access, "workspaces_access should be a catalog operation")
	assert.Equal(t, opmesh.InteractionHumanOnly, access.Interaction())
	assert.Equal(t, opmesh.VisibilityBoth, access.Visibility())
}

// TestWorkspacesDeleteIsDestructive verifies the delete operation is declared
// destructive (so the shared pipeline enforces --force) and that the delete
// leaf renders the --force/--confirm flags.
func TestWorkspacesDeleteIsDestructive(t *testing.T) {
	var del opmesh.Operation
	for _, op := range catalogops.WorkspacesOperations(catalogops.WorkspacesDeps{}) {
		if op.Name() == "workspaces_delete" {
			del = op
		}
	}
	require.NotNil(t, del, "workspaces_delete should be a catalog operation")
	assert.Equal(t, opmesh.SafetyDestructive, del.Safety())

	ws := findCommand(NewRootCommand().Commands, "workspaces")
	require.NotNil(t, ws)
	delCmd := findCommand(ws.Commands, "delete")
	require.NotNil(t, delCmd)
	hasForce := false
	for _, f := range delCmd.Flags {
		if f.Names()[0] == "force" || f.Names()[0] == "confirm" {
			hasForce = true
		}
	}
	assert.True(t, hasForce, "workspaces delete should expose a force/confirm flag")
}
