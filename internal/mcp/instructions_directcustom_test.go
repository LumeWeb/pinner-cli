package mcp

// Regression tests for the completed-surface instruction exposure: the
// initialize instructions must derive every tool-naming slot from the ONE
// canonical completed-surface membership predicate (catalogToolAvailable —
// catalog entries PLUS DirectCustom direct-only registrations), never a
// Get-only closure that would drop the no-app-mode direct-only
// upload_file/vault_put_file registrations from the instructions while the
// same server lists them directly on the wire.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
)

// TestInstructionsIncludeDirectCustomSurfaceTools pins the CRITICAL exposure
// regression: a hosted-style assembly whose upload/vault primitives were
// registered ONLY as direct custom tools (the roleDirectTool path recorded in
// catalog.DirectCustom) must still have its instruction tail name both tools —
// exactly the direct wire-visible surface. Under the repaired predicate the
// tail claims them; a catalog.Get-only closure would silently omit them.
func TestInstructionsIncludeDirectCustomSurfaceTools(t *testing.T) {
	cat := NewToolCatalog()
	cat.DomainScope = HostedDomainScope
	cat.Hosted = true
	for _, n := range []string{"auth_status", "pins_list"} {
		cat.Add(&model.ToolEntry{Name: n, Description: n + " description"})
	}
	// Direct-only registrations OUTSIDE catalog indexing (never catalog
	// entries): the no-app-mode upload/vault direct tools.
	cat.DirectCustom = []string{"upload_file", "vault_put_file"}
	require.Equal(t, 2, cat.Len(), "DirectCustom tools are not catalog entries")

	inst := cat.Instructions()

	require.Contains(t, inst,
		"File attachments can use the directly visible upload_file (IPFS) and vault_put_file (vault) tools over the banner-visible source modes;",
		"the instruction tail must name the direct-only upload_file/vault_put_file the surface exposes on the wire")
	require.Contains(t, inst, "TUS is never anonymous.",
		"the TUS note follows the upload-file claims when at least one upload family tool is exposed")
	require.Equal(t, cat.Len(), instructionToolCount(t, inst),
		"the tail count stays the indexed catalog count (direct-only tools are registered, not indexed)")

	// Negative control: a catalog WITHOUT the direct-only registrations drops
	// the claims — the source is membership, not a hard-coded list.
	bare := NewToolCatalog()
	bare.DomainScope = HostedDomainScope
	for _, n := range []string{"auth_status", "pins_list"} {
		bare.Add(&model.ToolEntry{Name: n, Description: n + " description"})
	}
	bareInst := bare.Instructions()
	require.NotContains(t, bareInst, "upload_file",
		"without the direct-only registrations, the tail must not claim the upload tools")
}
