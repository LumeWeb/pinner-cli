// Progressive/flat
// materialization. These tests verify the listing-strategy branch through the
// real registration + tools/list materialization path (stampDirectTools stamps
// DirectVisible, RegisterOfficialDirectTools projects onto an official SDK
// server, RegisterOfficialMetaTools gates the discovery meta-tools), and read
// the result back off the actual wire via the MCP client ListTools call —
// never by comparing constants against themselves.
//
// It covers the behavior matrix: progressive vs flat × full (local) vs
// hosted, plus the meta-tool rule (present under progressive; present
// under flat only when IncludeMetaOnFlat), surface/host filtering
// (a surface-disabled op never appears, in either strategy), and the
// no-duplicate-tool-names invariant.

package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner/catalogops"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
)

// fullTestBundle is a catalog-deps bundle that enables every operation domain
// (non-nil domain deps), so the compiled catalog carries the full local
// surface including the Sia vault — the same fixture TestAssembleCatalogOps
// uses. Hosted surface assembly on the same bundle drops the vault domain.
func fullTestBundle() *CatalogDepsBundle {
	return &CatalogDepsBundle{
		Auth:       catalogops.AuthDeps{},
		Account:    catalogops.AccountDeps{},
		Vault:      catalogops.VaultDeps{},
		VaultSetup: catalogops.VaultDeps{},
		Pins:       catalogops.PinsDeps{},
		Websites:   catalogops.WebsitesDeps{},
		DNS:        catalogops.DNSDeps{},
		IPNS:       catalogops.IPNSDeps{},
		ENS:        catalogops.ENSDeps{},
		APIKeys:    catalogops.APIKeysDeps{},
		Operations: catalogops.OperationsDeps{},
		Admin:      catalogops.AdminDeps{},
	}
}

// buildStrategyServer assembles a real official MCP server for the given
// surface/hosted + listing strategy and returns it. It mirrors the production
// registration pipeline (AssembleCatalogOps → populateCatalogTools →
// stampDirectTools → RegisterOfficialDirectTools, plus the meta-tools that
// OfficialServerFromCatalog registers) so the test exercises materialization,
// not just the policy value. The listing policy is captured on the CATALOG —
// the sole production source, exactly as buildCatalog captures it from the
// withPolicy option — never via the deprecated construction-time globals.
// setConstructionGuards records/restores the deployment (surface/hosted)
// globals those still-shared seams read, and the test-only cardCatalogVar
// seam used by deriveServerCardTools, so values cannot leak between tests.
func setConstructionGuards(t *testing.T, surface DomainScope, hosted bool) {
	t.Helper()
	prevSurface, prevHosted := activeDomainScope(), activeHosted()
	prevCardCatalog := cardCatalogVar
	t.Cleanup(func() {
		SetDomainScope(prevSurface)
		SetHosted(prevHosted)
		cardCatalogVar = prevCardCatalog
	})
	SetDomainScope(surface)
	SetHosted(hosted)
}

// restoreConstructionGuards resets the construction-time deployment globals
// (surface/hosted, which production still writes for the prompt/guide DSL)
// and the test-only cardCatalogVar seam to their defaults when the test
// finishes, so state set during the test (including via BuildServer) cannot
// leak into sibling tests. The deprecated listing globals are not touched:
// production reads none of them, and each legacy test restores its own.
func restoreConstructionGuards(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetDomainScope(FullDomainScope)
		SetHosted(false)
		cardCatalogVar = nil
	})
}

func buildStrategyServer(t *testing.T, surface DomainScope, hosted bool, strategy ToolListingStrategy, includeMeta bool) *mcp.Server {
	t.Helper()

	cat, err := AssembleCatalogOps(fullTestBundle(), surface, hosted)
	require.NoError(t, err, "assemble catalog for surface")

	catalog := NewToolCatalog()
	// Record construction-time state before compiling so the startup profile
	// (startupProfile → activeDomainScope/activeHosted) agrees with the surface,
	// mirroring buildCatalog's projection of the withPolicy option.
	setConstructionGuards(t, surface, hosted)

	_, err = populateCatalogTools(catalog, cat)
	require.NoError(t, err, "populate catalog surface")

	// Capture the listing policy on the catalog BEFORE stampDirectTools, exactly as
	// buildCatalog does: stampDirectTools and RegisterOfficialMetaTools must branch
	// on this per-catalog policy, not on any global.
	catalog.Strategy = strategy
	catalog.IncludeMetaOnFlat = includeMeta
	stampDirectTools(catalog)
	// Record the materialized catalog on the test-only card seam so the static
	// server card derives its flat direct surface from the actual registered
	// tools. The test helper's setConstructionGuards cleanup restores it.
	cardCatalogVar = catalog

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialDirectTools(srv, catalog))
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))
	return srv
}

// materializedNames lists the direct tools on the wire by connecting a real
// client and calling ListTools. It also asserts every name is unique (the
// no-duplicate invariant) and returns both the name set and the raw
// slice for size assertions.
func materializedNames(t *testing.T, srv *mcp.Server) (map[string]bool, []*mcp.Tool) {
	t.Helper()
	cs := connectOfficialClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err, "ListTools")

	uniq := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		uniq[tool.Name] = true
	}
	require.Equal(t, len(res.Tools), len(uniq),
		"tools/list must not contain duplicate tool names: got %d unique names for %d tools",
		len(uniq), len(res.Tools))
	return uniq, res.Tools
}

// presentNames is the set of model-visible ops in the catalog for a surface
// (the materializable set). DomainScope-disabled ops are absent by construction.
func presentNames(t *testing.T, surface DomainScope, hosted bool) map[string]bool {
	t.Helper()
	cat, err := AssembleCatalogOps(fullTestBundle(), surface, hosted)
	require.NoError(t, err)
	catalog := NewToolCatalog()
	_, err = populateCatalogTools(catalog, cat)
	require.NoError(t, err)
	names := make(map[string]bool, catalog.Len())
	for _, e := range catalog.Entries() {
		names[e.Name] = true
	}
	return names
}

// directPresent returns the curated set intersected with the ops actually
// materialized for a surface — what progressive stampDirectTools actually stamps.
func directPresent(surface DomainScope, present map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, n := range directToolNamesFor(surface) {
		if present[n] {
			out[n] = true
		}
	}
	return out
}

// flatCapableNames returns the names of materialized catalog ops that flat
// materialization may put directly on tools/list — exactly the ops agentDirectSafe
// accepts (not admin, wizard-category, or interactive). The full surface
// materializes admin ops, so this is strictly smaller than the materialized set;
// hosted surfaces carry no admin ops (and compiled ops are agent-safe), so for
// hosted it equals the materialized set.
func flatCapableNames(t *testing.T, surface DomainScope, hosted bool) map[string]bool {
	t.Helper()
	cat, err := AssembleCatalogOps(fullTestBundle(), surface, hosted)
	require.NoError(t, err)
	catalog := NewToolCatalog()
	_, err = populateCatalogTools(catalog, cat)
	require.NoError(t, err)
	names := make(map[string]bool, catalog.Len())
	for _, e := range catalog.Entries() {
		if agentDirectSafe(e) {
			names[e.Name] = true
		}
	}
	return names
}

// gatedMaterializedNames returns the names of materialized ops the flat safety
// carve-out keeps off tools/list (admin, wizard-category, or interactive). It is
// the exact complement of flatCapableNames, so the two partition the materialized
// surface.
func gatedMaterializedNames(t *testing.T, surface DomainScope, hosted bool) map[string]bool {
	t.Helper()
	cat, err := AssembleCatalogOps(fullTestBundle(), surface, hosted)
	require.NoError(t, err)
	catalog := NewToolCatalog()
	_, err = populateCatalogTools(catalog, cat)
	require.NoError(t, err)
	names := make(map[string]bool)
	for _, e := range catalog.Entries() {
		if !agentDirectSafe(e) {
			names[e.Name] = true
		}
	}
	return names
}

func TestProgressiveMaterializesDirectPlusMetaOnly(t *testing.T) {
	surface := FullDomainScope
	present := presentNames(t, surface, false)
	curated := directPresent(surface, present)
	// A curated name must actually be materializable for this invariant test to
	// be meaningful.
	require.NotEmpty(t, curated, "full surface must materialize at least one curated op")
	// pins_add is a primary/onboarding tool but NOT curated — it must stay
	// search-only under progressive.
	require.True(t, present["pins_add"], "pins_add must be a materialized (search-only) op for this fixture")
	require.False(t, curated["pins_add"], "pins_add must not be curated/direct")

	names, _ := materializedNames(t, buildStrategyServer(t, surface, false, ListingProgressive, false))

	// Curated direct tools are on tools/list.
	for n := range curated {
		require.Truef(t, names[n], "progressive must list curated tool %q", n)
	}
	// Meta tools are present (always, under progressive).
	for _, n := range metaToolNames {
		require.Truef(t, names[n], "progressive must list meta tool %q", n)
	}
	// The search-only op is NOT on tools/list — that is the whole point of
	// progressive disclosure.
	require.False(t, names["pins_add"], "pins_add must stay search-only under progressive")

	// tools/list is exactly curated + meta (no hidden/extra ops, no truncation).
	require.Len(t, names, len(curated)+len(metaToolNames))
}

func TestFlatMaterializesEverySafeCatalogOpDirect(t *testing.T) {
	surface := FullDomainScope
	flat := flatCapableNames(t, surface, false)
	gated := gatedMaterializedNames(t, surface, false)

	// The safety carve-out must actually be exercised: the full surface
	// materializes admin ops that flat must NOT turn into direct tools.
	require.NotEmpty(t, gated, "full surface must materialize gated (admin/wizard/interactive) ops for this invariant to be meaningful")
	require.NotEmpty(t, flat, "full surface must also materialize agent-safe ops to materialize directly")

	// Flat with the explicit IncludeMetaOnFlat=false override: every agent-safe
	// materialized op is direct; gated ops and meta tools are absent (the
	// intentional override that hides gated admin/wizard/interactive ops from the
	// channel). This is NOT the default — see TestFlatDefaultKeepsMetaTools.
	names, _ := materializedNames(t, buildStrategyServer(t, surface, false, ListingFlat, false))
	require.Equal(t, len(flat), len(names),
		"flat tools/list must equal the agent-safe materialized catalog exactly (no truncation of the safe surface)")
	for n := range flat {
		require.Truef(t, names[n], "flat must materialize agent-safe op %q directly", n)
	}
	// The safety carve-out: admin/wizard/interactive ops must NOT be direct.
	// Direct registration wires a tool's own handler straight onto tools/list,
	// bypassing the invoke-dispatch admin refusal / needs_human gates, so flat
	// keeps them behind the meta-tools (same protected behavior as progressive).
	for n := range gated {
		require.Falsef(t, names[n], "flat must NOT materialize gated op %q directly", n)
	}
	for _, n := range metaToolNames {
		require.Falsef(t, names[n], "flat override (IncludeMetaOnFlat=false) must NOT list meta tool %q", n)
	}

	// The search-only op that progressive keeps hidden is now direct.
	require.True(t, names["pins_add"], "flat must surface the formerly search-only pins_add")
	require.False(t, gated["pins_add"], "pins_add is agent-safe and must not be gated")
}

func TestFlatIncludeMetaOnFlatKeepsMetaTools(t *testing.T) {
	surface := FullDomainScope
	flat := flatCapableNames(t, surface, false)
	gated := gatedMaterializedNames(t, surface, false)

	names, _ := materializedNames(t, buildStrategyServer(t, surface, false, ListingFlat, true))
	require.Equal(t, len(flat)+len(metaToolNames), len(names),
		"flat with IncludeMetaOnFlat must materialize the safe catalog plus the meta tools")
	for n := range flat {
		require.Truef(t, names[n], "flat must materialize agent-safe op %q directly", n)
	}
	// The carve-out holds with meta tools present too: gated ops stay non-direct.
	for n := range gated {
		require.Falsef(t, names[n], "flat with IncludeMetaOnFlat must NOT materialize gated op %q directly", n)
	}
	for _, n := range metaToolNames {
		require.Truef(t, names[n], "flat with IncludeMetaOnFlat must keep meta tool %q", n)
	}
}

// TestFlatStrategyIndependentOfHosted pins strategy independence: a hosted server
// can be progressive or flat, and flat honors the hosted surface's filtering
// (no Sia vault ops) exactly as progressive does.
func TestFlatStrategyIndependentOfHosted(t *testing.T) {
	hostedPresent := presentNames(t, HostedDomainScope, true)

	// DomainScope filtering is respected: the hosted surface has no vault ops, so
	// neither strategy can ever materialize one. This is the hidden-op
	// invariant at the registration boundary.
	for _, n := range []string{"vault_status", "vault_create", "vault_restore", "vault_ls"} {
		require.Falsef(t, hostedPresent[n], "hosted surface must not materialize vault op %q", n)
	}

	// Flat hosted: exactly the hosted catalog is direct; no vault, no meta
	// (default).
	hostedFlat, _ := materializedNames(t, buildStrategyServer(t, HostedDomainScope, true, ListingFlat, false))
	require.Equal(t, len(hostedPresent), len(hostedFlat), "flat hosted must equal the hosted catalog exactly")
	for n := range hostedPresent {
		require.Truef(t, hostedFlat[n], "flat hosted must materialize %q", n)
	}
	require.False(t, hostedFlat["vault_status"], "flat hosted must not surface a vault op (surface-disabled)")

	// Full flat materializes the vault domain the hosted catalog excludes,
	// proving the hosted/full distinction is preserved under flat.
	fullFlat, _ := materializedNames(t, buildStrategyServer(t, FullDomainScope, false, ListingFlat, false))
	require.True(t, fullFlat["vault_status"], "full flat must materialize vault_status (hosted surface gated it)")

	// Hosted progressive is also independent: curated 3-set + meta, no vault.
	hostedProg, _ := materializedNames(t, buildStrategyServer(t, HostedDomainScope, true, ListingProgressive, false))
	for _, n := range append(directToolNamesFor(HostedDomainScope), metaToolNames...) {
		require.Truef(t, hostedProg[n], "hosted progressive must list %q", n)
	}
	require.False(t, hostedProg["vault_status"], "hosted progressive must not surface a vault op")
	require.False(t, hostedProg["pins_add"], "hosted progressive keeps search-only ops hidden")
}

// TestFlatNeverInventOpsOutsideCatalog pins that flat materialization never
// truncates but also never invents: the wire surface is exactly the ops the
// surface assembly materialized (plus meta only when opted in). A name absent
// from the catalog — simulating a hidden/feature-gated op — can never appear.
func TestFlatNeverInventOpsOutsideCatalog(t *testing.T) {
	surface := FullDomainScope
	present := presentNames(t, surface, false)

	// A hypothetical hidden op that the surface did NOT materialize (e.g. a
	// feature-gated capability absent from this transport profile). It must be
	// absent from the flat wire because flat surfaces only what the catalog
	// holds.
	hiddenOp := "never_materialized_op"
	require.False(t, present[hiddenOp], "fixture must not materialize the hidden op")

	for _, include := range []bool{false, true} {
		names, _ := materializedNames(t, buildStrategyServer(t, surface, false, ListingFlat, include))
		require.Falsef(t, names[hiddenOp], "flat must not invent op %q (includeMeta=%v)", hiddenOp, include)
	}
}

// TestFlatCustomDirectToolNoDuplicateToolName pins the no-duplicate
// invariant in the exact scenario flat creates: a custom/direct tool that is
// both a catalog entry (indexed) and registered through the direct descriptor
// path (RegisterOfficialDescriptor) — under flat, stampDirectTools stamps it
// DirectVisible so RegisterOfficialDirectTools would register it, and the
// custom pipeline also registers it directly. Registering the same name twice
// must collapse to ONE tool on the wire (map-idempotent registration), never a
// duplicate.
// TestWithPolicyProjectsListingAxesAndNotDeployment pins the Step 3/4
// construction seam: the withPolicy buildCatalogOpt carries the LISTING axes
// (strategy, meta-on-flat when explicit, onboarding) into the catalog build
// config and rejects an unsupported strategy at construction instead of
// silently falling back. It also pins the MEDIUM-2 guard: withPolicy must NOT
// touch cfg.surface / cfg.hosted, so a partial listing policy can never erase
// the deployment axes set by withDomainScope/withHosted.
func TestWithPolicyProjectsListingAxesAndNotDeployment(t *testing.T) {
	cfg := &buildCatalogConfig{}
	flat := DefaultPolicy()
	flat.Strategy = ListingFlat
	flat.IncludeMetaOnFlat = boolPtr(true)

	require.NoError(t, withPolicy(flat)(cfg))
	require.Equal(t, ListingFlat, cfg.strategy, "strategy must be projected")
	require.True(t, cfg.resolveIncludeMetaOnFlat(), "meta-on-flat must be projected")

	// MEDIUM-2 guard: withPolicy must leave the deployment axes untouched.
	require.False(t, cfg.hosted, "withPolicy must not set hosted from the policy")
	require.True(t, cfg.surface.IsZero(), "withPolicy must not set surface from the policy")

	// Deployment axes set independently (withDomainScope/withHosted) survive a
	// subsequent partial policy.
	cfg2 := &buildCatalogConfig{}
	require.NoError(t, withDomainScope(HostedDomainScope)(cfg2))
	require.NoError(t, withHosted(true)(cfg2))
	require.NoError(t, withPolicy(ListingPolicy{Strategy: ListingFlat})(cfg2))
	require.Equal(t, HostedDomainScope, cfg2.resolveDomainScope(), "partial policy must not erase withDomainScope")
	require.True(t, cfg2.hosted, "partial policy must not erase withHosted")

	// An unsupported strategy fails loudly at construction (Validate gate).
	invalid := DefaultPolicy()
	invalid.Strategy = ToolListingStrategy(200) // within uint8 range, not a supported strategy
	err := withPolicy(invalid)(cfg)
	require.Error(t, err, "unsupported strategy must be rejected at construction")
	require.Contains(t, err.Error(), "unsupported listing strategy")
}

// TestBuildServerPolicyFlatEndToEnd exercises the Policy construction seam
// through the real BuildServer assembly: a flat policy (no meta-on-flat)
// yields a fully-direct tools/list through the production pipeline, proving
// the policy value reaches materialization rather than being a dead field.
func TestBuildServerPolicyFlatEndToEnd(t *testing.T) {
	// BuildServer projects the policy onto the construction-time globals; make
	// sure that never leaks into later tests in the package.
	restoreConstructionGuards(t)

	flat := DefaultPolicy()
	flat.Strategy = ListingFlat
	flat.IncludeMetaOnFlat = boolPtr(false) // explicit opt-in to hide meta tools

	cfg := ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:      &flat,
		// Mirror what the CLI/hosted wiring's registerCustomTools does in the
		// custom registry pipeline (custom_tools_register.go run): stamp the
		// curated surface then project the direct tools onto tools/list.
		RegisterCustom: func(srv *sdk.Server, catalog *ToolCatalog) error {
			stampDirectTools(catalog)
			return RegisterOfficialDirectTools(srv, catalog)
		},
	}

	srv, catalog, err := BuildServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, catalog)

	names, _ := materializedNames(t, srv)
	require.True(t, names["pins_add"], "BuildServer flat must surface the formerly search-only pins_add")
	// Flat default excludes meta tools (OfficialServerFromCatalog skips them).
	for _, n := range metaToolNames {
		require.Falsef(t, names[n], "BuildServer flat default must not list meta tool %q", n)
	}
}

// TestBuildServerPolicyProgressiveDefaults pins that BuildServer with no
// Policy keeps current behavior: progressive listing + meta tools, with the
// curated set direct and search-only ops hidden.
func TestBuildServerPolicyProgressiveDefaults(t *testing.T) {
	restoreConstructionGuards(t)
	cfg := ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		RegisterCustom: func(srv *sdk.Server, catalog *ToolCatalog) error {
			stampDirectTools(catalog)
			return RegisterOfficialDirectTools(srv, catalog)
		},
	}

	srv, _, err := BuildServer(cfg)
	require.NoError(t, err)

	names, _ := materializedNames(t, srv)
	// Meta tools present under the default progressive strategy.
	for _, n := range metaToolNames {
		require.Truef(t, names[n], "default BuildServer must list meta tool %q", n)
	}
	// Search-only ops stay hidden under progressive.
	require.False(t, names["pins_add"], "default BuildServer must keep pins_add search-only")
}

func TestFlatCustomDirectToolNoDuplicateToolName(t *testing.T) {
	// Clean construction-time state for a full-surface flat server (restored
	// on cleanup).
	setConstructionGuards(t, FullDomainScope, false)

	catalog := NewToolCatalog()
	catalog.Strategy = ListingFlat
	catalog.IncludeMetaOnFlat = false
	// A custom/direct tool that is ALSO a catalog-indexed entry (the
	// upload_file / download_file / capabilities pattern in custom_tools.go).
	catalog.Add(&model.ToolEntry{
		Name:        "upload_file",
		Description: "Upload a file",
		InputSchema: []byte(`{"type":"object"}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		},
	})
	stampDirectTools(catalog)

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialDirectTools(srv, catalog))
	require.NoError(t, RegisterOfficialDescriptor(srv, model.ToolDescriptor{
		Name:        "upload_file",
		Description: "Upload a file",
		InputSchema: []byte(`{"type":"object"}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		},
	}))

	names, res := materializedNames(t, srv)
	require.True(t, names["upload_file"], "upload_file must be materialized")
	// Exactly one wire tool named upload_file (assert via the unique-name check
	// inside materializedNames plus an explicit count).
	var count int
	for _, tool := range res {
		if tool.Name == "upload_file" {
			count++
		}
	}
	require.Equal(t, 1, count, "custom direct tool must appear exactly once on tools/list despite double registration")
}

// TestServerConfigPartialPolicyPreservesDeploymentAxes is the MEDIUM-2
// regression: a ServerConfig that sets the deployment axes (Hosted:true,
// DomainScope:HostedDomainScope) PLUS a PARTIAL listing policy selecting only a
// strategy must NOT have its Hosted/DomainScope silently erased by the policy. The
// policy is listing-only, so the deployment axes survive and both are applied.
func TestServerConfigPartialPolicyPreservesDeploymentAxes(t *testing.T) {
	restoreConstructionGuards(t)

	// A partial listing policy: only the strategy is selected; no meta/onboarding.
	partial := ListingPolicy{Strategy: ListingFlat}

	cfg := ServerConfig{
		Hosted:      true,
		DomainScope: HostedDomainScope,
		Policy:      &partial,
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
	}

	srv, catalog, err := BuildServer(cfg)
	require.NoError(t, err, "BuildServer with a partial policy must succeed")
	require.NotNil(t, srv)
	require.Equal(t, HostedDomainScope, catalog.DomainScope,
		"partial listing policy must NOT erase ServerConfig.DomainScope")
	require.True(t, catalog.Hosted,
		"partial listing policy must NOT erase ServerConfig.Hosted")
	require.Equal(t, ListingFlat, catalog.Strategy,
		"the policy's strategy must still apply")
	// Flat mode omits discovery meta-tools unless explicitly enabled.
	require.False(t, catalog.IncludeMetaOnFlat,
		"partial flat policy must omit meta-tools by default")
}

// TestPartialFlatPolicyOmitsMetaByDefault is the BuildServer construction
// regression for flat policies that omit IncludeMetaOnFlat: discovery
// meta-tools stay off the wire unless a consumer explicitly enables them.
func TestPartialFlatPolicyOmitsMetaByDefault(t *testing.T) {
	restoreConstructionGuards(t)

	// Omitted IncludeMetaOnFlat omits meta tools from the wire.
	keepCfg := ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:      &ListingPolicy{Strategy: ListingFlat},
		RegisterCustom: func(srv *sdk.Server, catalog *ToolCatalog) error {
			stampDirectTools(catalog)
			return RegisterOfficialDirectTools(srv, catalog)
		},
	}
	keepSrv, _, err := BuildServer(keepCfg)
	require.NoError(t, err)
	keepNames, _ := materializedNames(t, keepSrv)
	for _, n := range metaToolNames {
		require.Falsef(t, keepNames[n], "partial flat policy (meta omitted) must omit meta tool %q from the wire", n)
	}

	// Explicit false produces the same flat surface.
	disable := ListingPolicy{Strategy: ListingFlat, IncludeMetaOnFlat: boolPtr(false)}
	hideCfg := ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:      &disable,
		RegisterCustom: func(srv *sdk.Server, catalog *ToolCatalog) error {
			stampDirectTools(catalog)
			return RegisterOfficialDirectTools(srv, catalog)
		},
	}
	hideSrv, _, err := BuildServer(hideCfg)
	require.NoError(t, err)
	hideNames, _ := materializedNames(t, hideSrv)
	for _, n := range metaToolNames {
		require.Falsef(t, hideNames[n], "explicit IncludeMetaOnFlat=false must NOT list meta tool %q", n)
	}
}

// TestLegacyListingGlobalsCannotAlterServerConstruction is the isolation
// regression test: buildCatalog no longer reads OR writes
// the deprecated listing globals (strategyVar/includeMetaOnFlatVar), so even a
// test or assembly path that leaves hostile values in them can never alter a
// server's construction, materialization, or instructions. A no-policy build
// keeps the default progressive listing with the SAFE meta-on-flat default
// (LOW-4), and an explicitly flat build materializes per its own policy —
// regardless of what the legacy globals say.
func TestLegacyListingGlobalsCannotAlterServerConstruction(t *testing.T) {
	restoreConstructionGuards(t)
	// Worst case: a legacy construction seam left the deprecated globals
	// pointing at a flat, meta-less server. These MUST NOT leak into either
	// build below.
	SetListingStrategy(ListingFlat)
	SetIncludeMetaOnFlat(false)

	registerDirect := func(srv *sdk.Server, catalog *ToolCatalog) error {
		stampDirectTools(catalog)
		return RegisterOfficialDirectTools(srv, catalog)
	}

	// 1. A no-policy build keeps the progressive strategy and progressive
	// wire surface. The flat-only meta setting remains disabled.
	noPolicyCfg := ServerConfig{
		CatalogDeps:    func() *CatalogDepsBundle { return fullTestBundle() },
		RegisterCustom: registerDirect,
	}
	srv, catalog, err := BuildServer(noPolicyCfg)
	require.NoError(t, err)
	require.Equal(t, ListingProgressive, catalog.Strategy,
		"legacy flat strategy global must not leak into a no-policy build")
	require.False(t, catalog.IncludeMetaOnFlat,
		"no-policy build must keep the disabled flat meta default")
	require.Contains(t, catalog.Instructions(), "intentionally two-tier",
		"a no-policy build's instructions must describe the progressive two-tier surface")
	names, _ := materializedNames(t, srv)
	for _, n := range metaToolNames {
		require.Truef(t, names[n], "no-policy build must list meta tool %q despite the hostile globals", n)
	}
	require.False(t, names["pins_add"],
		"no-policy build must keep pins_add search-only despite the hostile flat global")

	// 2. An explicitly flat, meta-less build still works and records its OWN
	// policy (captured from the policy value, not the global).
	flat := DefaultPolicy()
	flat.Strategy = ListingFlat
	flat.IncludeMetaOnFlat = boolPtr(false)
	flatCfg := ServerConfig{
		CatalogDeps:    func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:         &flat,
		RegisterCustom: registerDirect,
	}
	flatSrv, flatCatalog, err := BuildServer(flatCfg)
	require.NoError(t, err)
	require.Equal(t, ListingFlat, flatCatalog.Strategy)
	require.False(t, flatCatalog.IncludeMetaOnFlat)
	flatNames, _ := materializedNames(t, flatSrv)
	for _, n := range metaToolNames {
		require.Falsef(t, flatNames[n], "explicit IncludeMetaOnFlat=false must hide meta tool %q", n)
	}
	require.True(t, flatNames["pins_add"], "flat build must surface pins_add directly")
}

// TestHostReassemblyReusesStartupNormalizedConfig is the MEDIUM-3 regression:
// host-profile reassembly must reuse the STARTUP server's normalized build
// config (surface/hosted/strategy/meta/onboarding), so a negotiated per-host
// server cannot differ from the startup server on its deployment and listing
// axes. It exercises the captureServerBuild -> normalizedServerBuild.opts()
// reuse seam exactly as the tunnel hostServerFactory does, and compares the
// resulting tool/card behavior.
func TestHostReassemblyReusesStartupNormalizedConfig(t *testing.T) {
	restoreConstructionGuards(t)

	// Startup server built with a non-default listing policy + onboarding.
	startup := ListingPolicy{
		Strategy:          ListingFlat,
		IncludeMetaOnFlat: boolPtr(true),
		Onboarding:        []string{"auth_status", "websites_create"},
	}
	startupCatalog, err := buildCatalog(nil, nil, nil, nil, nil, nil,
		withCatalogDeps(func() *CatalogDepsBundle { return fullTestBundle() }),
		withDomainScope(HostedDomainScope),
		withHosted(true),
		withPolicy(startup),
	)
	require.NoError(t, err, "build startup catalog")

	// Capture the normalized build config (the MEDIUM-3 reuse seam).
	normalized := captureServerBuild(startupCatalog)
	require.Equal(t, HostedDomainScope, normalized.surface)
	require.True(t, normalized.hosted, "startup hosted must be captured")

	// A "negotiated" per-host reassembly reuses normalized.opts() exactly as
	// hostServerFactory appends it to catalogOpts.
	negotiatedOpts := []buildCatalogOpt{
		withCatalogDeps(func() *CatalogDepsBundle { return fullTestBundle() }),
	}
	negotiatedOpts = append(negotiatedOpts, normalized.opts()...)
	negotiatedCatalog, err := buildCatalog(nil, nil, nil, nil, nil, nil, negotiatedOpts...)
	require.NoError(t, err, "build negotiated catalog")

	// Deployment axes + listing policy must be identical between the two.
	require.Equal(t, startupCatalog.DomainScope, negotiatedCatalog.DomainScope)
	require.Equal(t, startupCatalog.Hosted, negotiatedCatalog.Hosted)
	require.Equal(t, startupCatalog.Strategy, negotiatedCatalog.Strategy)
	require.Equal(t, startupCatalog.IncludeMetaOnFlat, negotiatedCatalog.IncludeMetaOnFlat)
	require.Equal(t, startupCatalog.OnboardingOverride, negotiatedCatalog.OnboardingOverride)

	// Tool/card behavior identical between startup and negotiated servers.
	require.Equal(t,
		serverCardNames(NewServerCard(startupCatalog).Tools()),
		serverCardNames(NewServerCard(negotiatedCatalog).Tools()),
		"negotiated (host-profile) server card must equal the startup server card")
}
