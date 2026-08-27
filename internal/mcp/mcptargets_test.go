package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// requireMCPTargets asserts a tool descriptor declares a per-profile MCP target
// list. Every registered tool — catalog-indexed or direct — must carry one so
// describe_tool/search_tools resolve descriptions through the same seam.
func requireMCPTargets(t *testing.T, desc model.ToolDescriptor) {
	t.Helper()
	require.NotEmpty(t, desc.MCPTargets, "tool %q must declare MCPTargets", desc.Name)
}

// TestToolRegistrationsCarryMCPTargets is a static inventory of every custom
// tool registration path. It guards the invariant that no tool is registered
// without a profile-aware target list.
func TestToolRegistrationsCarryMCPTargets(t *testing.T) {
	// Direct/meta tools (not catalog-indexed) must still declare MCPTargets.
	requireMCPTargets(t, NewCapabilitiesDescriptor(false, false, true, true, true, true, true, true, true, true, 1<<20, hostenv.ProfileOpenAITunnel.Features))
	requireMCPTargets(t, NewAgentGuideDescriptor())

	// Transport tools.
	requireMCPTargets(t, transfer.NewUploadFileDescriptor(transportFeatures(true, false), true, false, nil, nil, nil, nil, 0))
	requireMCPTargets(t, transfer.NewDownloadFileDescriptor(nil, nil, "", 0, false))
	requireMCPTargets(t, transfer.DataURIUploadDescriptor(nil, 0))
	requireMCPTargets(t, vault.NewVaultPutFileDescriptor(transportFeatures(true, false), true, false, nil, nil, nil, nil, 0))
	requireMCPTargets(t, vault.NewVaultGetFileDescriptor(nil, nil, "", 0, false))
	requireMCPTargets(t, upload.RelayURLUploadDescriptor(nil, nil, 0))

	// Upload-manager launchers.
	requireMCPTargets(t, upload.NewOpenUploadManagerDescriptor(nil))
	requireMCPTargets(t, upload.NewOpenVaultManagerDescriptor(nil))

	// UI launchers (the apps.NewOpenLauncherDescriptor path covers every
	// open_* launcher).
	requireMCPTargets(t, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{Name: "open_test", Description: "x"}))

	// Auth / resume tools. Vault + auth resumes share handoff.NewResumeTool,
	// so exercising it once covers auth_resume / vault_create_resume /
	// vault_restore_resume.
	requireMCPTargets(t, auth.NewAuthSSODescriptor(nil, nil, nil))
	requireMCPTargets(t, handoff.NewResumeTool(handoff.ResumeToolSpec{Name: "resume_tool", Description: "x"}, nil, nil))
	requireMCPTargets(t, auth.NewAccountPasswordUpdateDescriptor(nil, nil, nil, nil))
	requireMCPTargets(t, auth.NewAccountPasswordResetDescriptor(nil, ""))
	requireMCPTargets(t, auth.NewAccountEmailChangeDescriptor(nil, nil))
	requireMCPTargets(t, oob.NewVaultCreateResumeDescriptor(nil, nil))
	requireMCPTargets(t, oob.NewVaultRestoreResumeDescriptor(nil, nil))
}

// TestCatalogAddGuaranteesMCPTargets asserts the central normalization: any
// entry added to the catalog without a target list gets a universal Fallback
// wrapping its static Description, so no catalog tool ever resolves with an
// empty MCPTargets. Resolution of a Fallback returns the static Description.
func TestCatalogAddGuaranteesMCPTargets(t *testing.T) {
	cat := NewToolCatalog()
	cat.Add(&model.ToolEntry{Name: "bare", Description: "no targets declared"})

	entry, ok := cat.Get("bare")
	require.True(t, ok)
	require.NotEmpty(t, entry.MCPTargets)

	desc, ok := toolforge.ResolveDescription(entry.MCPTargets, hostenv.ProfileHTTPGeneric)
	require.True(t, ok)
	require.Equal(t, "no targets declared", desc)
}
