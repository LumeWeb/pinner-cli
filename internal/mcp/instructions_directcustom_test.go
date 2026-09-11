package mcp

// Regression tests for the completed-surface instruction exposure: the
// initialize instructions must derive every tool-naming slot from the ONE
// canonical completed-surface membership predicate (catalogToolAvailable —
// catalog entries PLUS DirectCustom direct-only registrations), never a
// Get-only closure that would drop the no-app-mode direct-only
// upload_file/vault_put_file registrations from the instructions while the
// same server lists them directly on the wire.

import (
	"context"
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

// TestInstructionsIncludeDirectCustomInRealBuildServer pins the REAL
// construction-path regression the direct-only instruction exposure depends
// on: when a server is assembled through the production one-pass pipeline
// (BuildServer -> CollectExtensions collecting the extension plan -> complete()
// -> Instructions() derived BEFORE the single Materialize pass), the
// no-app-mode direct-only tools (registered as roleDirectTool, never
// catalog-indexed) MUST still be recorded on catalog.DirectCustom by the end
// of the COLLECTION phase — otherwise the instructions built immediately after
// it (which consult catalogToolAvailable's DirectCustom fallback while the
// finalized surface is still nil) silently drop exactly the tools the same
// server then lists on the wire. This is deliberately NOT the tautological
// manual-set path (TestInstructionsIncludeDirectCustomSurfaceTools): it drives
// the registry's own complete() via BuildServer and asserts the observable
// construction outcomes — the DirectCustom record, the initialize-instruction
// copy, and the tools/list registration.
func TestInstructionsIncludeDirectCustomInRealBuildServer(t *testing.T) {
	restoreConstructionGuards(t)

	// Direct-only no-app registrations, declared through the real extension
	// registry the way collectServerExtensions does (roleDirectTool, never
	// roleCatalogSearch): absent from the catalog, present only via the
	// DirectCustom side channel until Materialize finalizes the surface.
	directOnlyNames := []string{"upload_file", "vault_put_file"}

	srv, catalog, err := BuildServer(ServerConfig{
		// buildCatalog requires the compiler-backed surface bundle; the catalog
		// ops it provides are immaterial to this assertion (the direct-only
		// specs are layered on top), but the production Builder requires it.
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		CollectExtensions: func(cat *ToolCatalog) (*MaterializationPlan, error) {
			reg := newServerExtensionRegistry(nil, cat)
			for _, n := range directOnlyNames {
				reg.add(directOnly(roleTestTool(n)))
			}
			// The production collection seam: complete() validates, indexes
			// searchable specs, and (the repaired behavior) records the
			// direct-only tools on DirectCustom before the plan is returned.
			if err := reg.complete(); err != nil {
				return nil, err
			}
			return &MaterializationPlan{reg: reg}, nil
		},
	})
	require.NoError(t, err, "real BuildServer + CollectExtensions assembly must succeed")
	require.NotNil(t, catalog)
	require.NotNil(t, srv)

	// Observable outcome 1: the direct-only tools were recorded during the
	// COLLECTION phase (before Materialize ran), so the instructions computed
	// from the completed catalog can name them.
	require.Contains(t, catalog.DirectCustom, "upload_file",
		"collection phase must record the direct-only upload_file on DirectCustom for the pre-materialize instructions")
	require.Contains(t, catalog.DirectCustom, "vault_put_file",
		"collection phase must record the direct-only vault_put_file on DirectCustom for the pre-materialize instructions")

	// Observable outcome 2: the initialize instructions derive from that
	// completed surface and name BOTH direct-only tools.
	cs := connectOfficialClient(t, srv)
	inst := cs.InitializeResult().Instructions
	require.NotEmpty(t, inst, "assembled server must carry initialize instructions")
	require.Contains(t, inst, "directly visible upload_file (IPFS) and vault_put_file (vault) tools",
		"initialize instructions must name the direct-only upload_file/vault_put_file the same server advertises")
	require.Equal(t, catalog.Len(), instructionToolCount(t, inst),
		"the tail count stays the indexed catalog count (direct-only tools are registered, not indexed)")

	// Observable outcome 3: the same tools are actually registered on
	// tools/list — instructions and wire surface never diverge.
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err, "ListTools")
	wire := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		wire[tool.Name] = true
	}
	for _, n := range directOnlyNames {
		require.Truef(t, wire[n], "direct-only tool %q must be registered on tools/list", n)
	}
}
