package mcp

// One-pass per-server tooling materialization regression tests. They drive
// the REAL construction path — BuildServer/BuildHostedServer with the
// CollectExtensions pipeline (collectServerExtensions + MaterializationPlan.Materialize)
// — and read the outcome off observable fact: the live wire tools/list, the
// frozen initialize instructions, the per-server ServerCard, and the finalized
// MaterializedTooling's own membership. They never compare the plan's internal
// shape against itself.

import (
	"context"
	"io"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/session"
	mctf "go.lumeweb.com/mcpplane/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
)

// instructionToolCount extracts the catalog tool count the initialize
// instructions embed ("The internal catalog has %d tools.").
func instructionToolCount(t *testing.T, instructions string) int {
	t.Helper()
	re := regexp.MustCompile(`The internal catalog has (\d+) tools\.`)
	m := re.FindStringSubmatch(instructions)
	require.NotNil(t, m, "instructions must carry the catalog tool count")
	n, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return n
}

// appHelperWireOnlyNames lists the app-only helper descriptors the production
// app installers register directly on the server (a separate visibility
// domain): they surface on the wire but never join the catalog, the finalized
// direct membership, or the card.
func appHelperWireOnlyNames() []string {
	return []string{
		"pin_status", "auth_sso_status",
		"ipfs_upload_submit", "ipfs_upload_status",
		"vault_upload_submit",
		"vault_create_status", "vault_restore_status",
	}
}

// countOnWire counts occurrences of name in a raw tools/list slice (duplicates
// would mean a double projection).
func countOnWire(t *testing.T, tools []*mcp.Tool, name string) int {
	t.Helper()
	n := 0
	for _, tool := range tools {
		if tool.Name == name {
			n++
		}
	}
	return n
}

// contains is a tiny readable alias for the slice membership checks below.
func contains(list []string, name string) bool {
	return slices.Contains(list, name)
}

// TestInstructionsCountCoversIndexedExtensions pins the instruction-fix regression:
// the initialize instructions are computed AFTER extension collection, so the
// count covers every indexed extension (wizard/dev provisions and searchable
// extensions), not just the compiled operation surface — INCLUDING the
// consolidated open_app launcher, which is indexed during the collection phase
// (its post-surface hook REPLACES its entry with a post-app-install
// descriptor; it never changes the catalog's size). The declared count must
// therefore equal the zero-exception, FINAL indexed catalog length.
func TestInstructionsCountCoversIndexedExtensions(t *testing.T) {
	restoreConstructionGuards(t)

	// 0. Baseline: a server whose CollectExtensions declares NO extensions
	// carries exactly the compiled operation surface. This anchors what
	// "extensions" contribute below.
	opsOnly, opsCat, err := BuildServer(ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		CollectExtensions: func(catalog *ToolCatalog) (*MaterializationPlan, error) {
			reg := newServerExtensionRegistry(nil, catalog)
			plan := &MaterializationPlan{reg: reg}
			if err := reg.complete(); err != nil {
				return nil, err
			}
			return plan, nil
		},
		SeedDrop:   oobpkg.NewSeedDrop(oobpkg.DefaultSeedDropTTL),
		OOBRestore: oobpkg.NewOOBRestore(&fakeRestoreRunner{profile: "default"}, oobpkg.DefaultRestoreTTL),
		OOBCreate: func() *oobpkg.OOBCreate {
			c, _, _ := buildCreateServer()
			return c
		}(),
		HandoffReg:  handoff.NewHandoffRegistry(),
		AuthHandles: session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions),
	})
	require.NoError(t, err, "extension-free assembly must build")
	_ = opsOnly
	opsCount := instructionToolCount(t, connectOfficialClient(t, opsOnly).InitializeResult().Instructions)
	require.Equal(t, opsCat.Len(), opsCount,
		"with no extensions, the instruction count must equal the compiled op surface")

	// 1. The FULL production collection path (BuildServer + CollectExtensions
	// + Materialize) indexes extensions; the instruction count must include
	// them and stay consistent with the final catalog.
	inv := buildInventoryServer(t, nil, nil)
	fullCount := instructionToolCount(t, inv.instructions)

	require.Greater(t, fullCount, opsCount,
		"the instruction count must include the indexed server extensions (wizard/dev provisions, searchable relays, OOB/account entries, launchers, op relays)")

	// The consolidated open_app launcher is indexed during the collection
	// phase (its post-surface hook replaces, not appends, its entry), so the
	// frozen count is exactly the final indexed membership — no exception.
	require.Equalf(t, inv.catalog.Len(), fullCount,
		"instructions must count the completed indexed catalog (final entries %d, no post-surface exception)",
		inv.catalog.Len())

	// Each indexed extension family contributes to the count: every one of
	// these catalog members is an EXTENSION index (provision or searchable
	// spec), never a compiled op, and its presence proves the collection phase
	// fed the instruction count.
	extensionIndexed := []string{
		"setup_wizard_start",   // wizard provision (beforeDirectTools)
		"auth_resume",          // OOB sign-in extension (searchable)
		"account_email_change", // account credential extension
		"vault_create_resume",  // vault handoff extension
		"upload_status",        // async upload management extension
		"capabilities",         // direct+searchable transport extension
		"agent_guide",          // direct+searchable guide extension
		"open_pin_creator",     // app launcher (indexed during collection)
		"upload_file",          // dual-role upload bridge (curlUpload wired)
	}
	for _, n := range extensionIndexed {
		_, ok := inv.catalog.Get(n)
		require.Truef(t, ok, "extension entry %q must be indexed in the completed catalog", n)
	}
}

// TestFinalSurfaceNoDuplicateProjectionForFlat pins the de-dup contract: under
// flat materialization the direct DirectVisible stamp and the explicit
// extension direct descriptors overlap (capabilities, agent_guide, the
// transport bridges are direct AND indexed), and the single materialization
// pass must register each tool exactly once — never rely on SDK map
// replacement.
func TestFinalSurfaceNoDuplicateProjectionForFlat(t *testing.T) {
	flatMeta := ListingPolicy{Strategy: ListingFlat, IncludeMetaOnFlat: boolPtr(true)}
	inv := buildInventoryServer(t, &flatMeta, nil)

	// sessionToolNames already asserts global wire uniqueness; the wire slice
	// lets us count the overlapping dual-role names explicitly.
	var wireTools []*mcp.Tool
	inv.wireNames, wireTools = sessionToolNames(t, inv.session)

	require.Truef(t, inv.wireNames["pins_add"], "flat must surface search-only pins_add directly")
	// Every junction the direct stamp and the extension direct pass share:
	for _, n := range []string{"capabilities", "agent_guide", "upload_file", "download_file", "vault_get_file", "vault_put_file"} {
		require.Equalf(t, 1, countOnWire(t, wireTools, n),
			"flat dual-curation %q must be projected exactly once (deduplicated before registration)", n)
		_, inCat := inv.catalog.Get(n)
		require.Truef(t, inCat || contains(inv.catalog.DirectCustom, n),
			"flat direct %q must resolve through the single membership (catalog entry or direct-only record)", n)
	}
	// The finalized surface's direct membership matches the productive wire:
	// every wire member that is neither a meta tool nor an app-only helper is
	// exactly one finalized direct tool.
	final := inv.catalog.FinalizedTooling()
	require.NotNil(t, final, "a plan-materialized assembly must record its finalized surface")
	for name := range inv.wireNames {
		if contains(metaToolNames, name) || contains(appHelperWireOnlyNames(), name) {
			continue
		}
		require.Truef(t, final.IsDirect(name), "flat wire tool %q must be a member of the finalized direct surface", name)
	}
}

// TestServerCardMatchesFinalSurface pins per-strategy card alignment through
// the real construction path: the progressive card is the direct final direct
// intersection + meta; the flat card is the whole finalized direct membership
// + meta (including direct custom tools); descriptions derive from the
// surface's own descriptor/entry copy. Hosted is exercised through the real
// BuildHostedServer constructor.
func TestServerCardMatchesFinalSurface(t *testing.T) {
	restoreConstructionGuards(t)

	// --- Progressive (full local) ---
	prog := buildInventoryServer(t, nil, nil)
	progCard := serverCardNames(prog.card.Tools())
	direct := directToolNamesFor(FullDomainScope)
	require.Equal(t, append(append([]string{}, direct...), metaToolNames...), progCard,
		"progressive card must be exactly the direct set + meta tools, in direct order")
	// The progressive card does NOT advertise the direct extensions outside the
	// direct name set (capabilities/agent_guide/upload_file/...) that the direct
	// wire deliberately carries — the long-standing card contract.
	for _, n := range []string{"capabilities", "agent_guide", "upload_file", "download_file", "vault_put_file"} {
		require.NotContainsf(t, progCard, n, "progressive card must not advertise a direct extension outside the direct name set: %q", n)
	}
	// Every card description is non-degenerate and derived from the surface.
	descs := serverCardDescriptions(prog.card.Tools())
	for _, n := range progCard {
		require.NotEmptyf(t, descs[n], "progressive card entry %q must carry a description", n)
	}

	// --- Flat (meta kept, full local) ---
	flatMeta := ListingPolicy{Strategy: ListingFlat, IncludeMetaOnFlat: boolPtr(true)}
	flat := buildInventoryServer(t, &flatMeta, nil)
	flatCard := serverCardNames(flat.card.Tools())
	final := flat.catalog.FinalizedTooling()
	require.NotNil(t, final, "flat plan-materialized assembly must record its finalized surface")

	want := map[string]bool{}
	for _, t2 := range final.DirectTools() {
		want[t2.Name] = true
	}
	require.Truef(t, final.MetaOnWire(), "flat with the safe meta default keeps the discovery meta tools")
	for _, m := range metaToolNames {
		want[m] = true
	}
	for _, n := range flatCard {
		require.Truef(t, want[n], "flat card entry %q must be a member of the finalized direct surface + meta", n)
	}
	require.Equalf(t, len(want), len(flatCard), "flat card must carry exactly the finalized direct membership + meta")
	require.Contains(t, flatCard, "pins_add", "flat card must advertise directly-visible pins_add")
	require.Contains(t, flatCard, "upload_file", "flat card must advertise the direct upload_file extension")
	for _, h := range appHelperWireOnlyNames() {
		require.NotContainsf(t, flatCard, h, "flat card must never advertise the app-only helper %q", h)
	}
	// Descriptions derive from the finalized surface: a direct custom tool that
	// is not in the compatibility table carries its own descriptor copy, not
	// the tool name itself.
	flatDescs := serverCardDescriptions(flat.card.Tools())
	require.NotEqual(t, "upload_file", flatDescs["upload_file"],
		"flat card must derive upload_file's description from its descriptor, not fall back to the name")
	if entry, ok := flat.catalog.Get("upload_file"); ok {
		require.Equal(t, entry.Description, flatDescs["upload_file"],
			"flat card description for an indexed direct tool must equal its catalog entry description")
	}
	// The flat wire minus app-only helpers is exactly the card membership.
	wire := flat.wireNames
	for name := range wire {
		if contains(metaToolNames, name) || contains(appHelperWireOnlyNames(), name) {
			continue
		}
		require.Containsf(t, flatCard, name, "flat wire tool %q must be card-advertised", name)
	}

	// --- Hosted (real BuildHostedServer path) ---
	srv, hostedCat, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Options: []MCPServerOption{
			WithPrompts(),
			WithUploadTaskManager(mctf.NewUploadTaskManager(stubUploadExec, 0)),
			WithIPFSDownload(transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil })),
		},
	})
	require.NoError(t, err, "real hosted assembly must build")
	hCard := NewServerCard(hostedCat)
	hNames := serverCardNames(hCard.Tools())
	require.Equal(t, append(append([]string{}, directToolNamesFor(HostedDomainScope)...), metaToolNames...), hNames,
		"hosted progressive card must be exactly the hosted direct set + meta")
	// No vault op is card-advertised on the hosted surface.
	for _, n := range []string{"vault_status", "vault_create", "vault_restore", "vault_put_file"} {
		require.NotContainsf(t, hNames, n, "hosted card must not advertise %q", n)
	}
	// Hosted instructions count the completed hosted catalog too (extensions
	// included, open_app indexed during collection).
	hCount := instructionToolCount(t, connectOfficialClient(t, srv).InitializeResult().Instructions)
	require.Equal(t, hostedCat.Len(), hCount,
		"hosted instructions must count the completed hosted catalog (final entries, no post-surface exception)")
	require.Greater(t, hCount, len(directToolNamesFor(HostedDomainScope)),
		"hosted instruction count must include the indexed hosted extensions (guide, capabilities, relays, launchers)")
}

// TestLegacyRegisterCustomCardFallback pins the documented compatibility seam:
// a server assembled through the legacy single-callback RegisterCustom path
// (no finalized surface) still derives its card from the legacy
// DirectVisible/DirectCustom derivation, and the finalized surface stays nil.
func TestLegacyRegisterCustomCardFallback(t *testing.T) {
	restoreConstructionGuards(t)

	flatNoMeta := ListingPolicy{Strategy: ListingFlat, IncludeMetaOnFlat: boolPtr(false)}
	// The legacy registration seam exactly as the pre-plan tests drive it.
	legacyDirect := func(srv *sdk.Server, catalog *ToolCatalog) error {
		stampDirectTools(catalog)
		return RegisterOfficialDirectTools(srv, catalog)
	}
	_, catalog, err := BuildServer(ServerConfig{
		CatalogDeps:    func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:         &flatNoMeta,
		RegisterCustom: legacyDirect,
	})
	require.NoError(t, err, "legacy RegisterCustom build must succeed")

	require.Nilf(t, catalog.FinalizedTooling(),
		"a legacy RegisterCustom assembly records no finalized surface (documented fallback)")
	card := NewServerCard(catalog)
	names := serverCardNames(card.Tools())
	require.NotEmpty(t, names, "legacy-derived card must still carry tools")
	require.Contains(t, names, "pins_add", "legacy flat card must advertise directly-visible pins_add")
	for _, m := range metaToolNames {
		require.NotContainsf(t, names, m, "legacy flat IncludeMetaOnFlat=false card must not advertise %q", m)
	}
}

// TestGuideAvailabilityReadsFinalizedDirectMembership pins the DRY fix in the
// guide's completed-surface membership predicate (catalogToolAvailable): when
// a finalized surface exists, a direct-only tool that lives ONLY in the
// MaterializedTooling's direct set (not a catalog member, absent from the
// DirectCustom side channel) is available to the guide — so a future
// direct-only projection can never be omitted by the agent guide while the
// server advertises it on tools/list. The DirectCustom side channel remains
// the record only for catalogs assembled without the plan (legacy path).
func TestGuideAvailabilityReadsFinalizedDirectMembership(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{Name: "searchable_tool", Description: "searchable op"})
	directOnly := &DirectTool{Name: "future_direct_only", Description: "direct-only op"}
	finalized := &MaterializedTooling{
		catalog:     catalog,
		direct:      []DirectTool{*directOnly},
		directIndex: map[string]DirectTool{"future_direct_only": *directOnly},
	}
	catalog.setFinalized(finalized)

	avail := catalogToolAvailable(catalog)
	require.True(t, avail("searchable_tool"), "a catalog member stays available to the guide")
	require.True(t, avail("future_direct_only"),
		"the guide must see a direct-only tool the finalized surface advertises even though it is absent from the catalog AND the DirectCustom side channel")
	require.False(t, avail("absent_tool"))

	// Legacy/no-finalized path: the DirectCustom side channel is the record.
	legacy := NewToolCatalog()
	legacy.Add(&model.ToolEntry{Name: "searchable_tool", Description: "searchable op"})
	legacy.DirectCustom = []string{"legacy_direct_only"}
	legacyAvail := catalogToolAvailable(legacy)
	require.True(t, legacyAvail("searchable_tool"))
	require.True(t, legacyAvail("legacy_direct_only"),
		"the legacy path keeps consulting the DirectCustom side channel for directly-registered tools")
	require.False(t, legacyAvail("absent_tool"))
}
