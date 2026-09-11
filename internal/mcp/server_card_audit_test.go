package mcp

// Regression tests for the remaining LOW audit findings:
//
//  1. Policy.Onboarding is threaded through catalog materialization: the
//     "start here" override actually drives ToolCatalog.Onboarding() end-to-end
//     (BuildServer → withPolicy → OnboardingOverride → Onboarding()) instead of
//     being parsed and ignored.
//  2. The progressive server card intersects curated names with the actually
//     materialized catalog entries, so it never advertises a curated name a
//     gated/incomplete assembly failed to materialize.
//  3. The server card is served from an immutable per-server ServerCard captured
//     at construction, so two servers built sequentially cannot cross-contaminate
//     through the mutable construction-time globals.
//  4. The flat server card includes direct custom tools registered outside
//     catalog indexing (e.g. upload_file), through the real serverExtensionRegistry
//     path, so the card matches the flat tools/list surface exactly.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
)

// buildCardTestCatalog assembles a real materialized ToolCatalog for the given
// policy and records its card state (Strategy / IncludeMetaOnFlat) so a
// ServerCard captured from it carries the right per-server values — mirroring
// buildCatalog's projection of withPolicy onto the catalog.
func buildCardTestCatalog(t *testing.T, surface DomainScope, hosted bool, strategy ToolListingStrategy, includeMeta bool) *ToolCatalog {
	t.Helper()
	ops, err := AssembleCatalogOps(fullTestBundle(), surface, hosted)
	require.NoError(t, err, "assemble catalog for surface")
	catalog := NewToolCatalog()
	setConstructionGuards(t, surface, hosted)
	catalog.DomainScope = surface
	catalog.Strategy = strategy
	catalog.IncludeMetaOnFlat = includeMeta
	_, err = populateCatalogTools(catalog, ops)
	require.NoError(t, err, "populate catalog surface")
	stampDirectTools(catalog)
	return catalog
}

// --- Finding 1: Onboarding override threaded end-to-end ---

func TestOnboardingOverrideThreadedEndToEnd(t *testing.T) {
	restoreConstructionGuards(t)
	pol := DefaultPolicy()
	// A deliberately interesting override: auth_status is a builtin primary,
	// websites_create is curated but NOT a builtin primary, and vault_create is
	// a builtin primary we deliberately drop to prove the override REPLACES the
	// builtin set rather than merely adding to it.
	pol.Onboarding = []string{"auth_status", "websites_create"}

	srv, cat, err := BuildServer(ServerConfig{
		Policy:      &pol,
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
	})
	require.NoError(t, err, "BuildServer with an Onboarding override must succeed")
	require.NotNil(t, srv)
	// The override must have been projected onto the catalog's onboarding path.
	require.Equal(t, []string{"auth_status", "websites_create"}, cat.OnboardingOverride)

	res := cat.Onboarding()
	names := make(map[string]bool, len(res.Tools))
	for _, tm := range res.Tools {
		names[tm.Name] = true
	}
	// Override members are onboarded.
	require.True(t, names["auth_status"], "override member auth_status must be onboarded")
	require.True(t, names["websites_create"], "override member websites_create must be onboarded")
	// A builtin primary that is NOT in the override must be absent: the override
	// replaces the builtin predicate, so vault_create / pins_add fall off.
	require.False(t, names["vault_create"], "builtin primary not in override must NOT be onboarded")
	require.False(t, names["pins_add"], "builtin primary not in override must NOT be onboarded")
}

// --- Finding 2: progressive card intersects materialized catalog ---

func TestProgressiveCardIntersectsMaterializedCatalog(t *testing.T) {
	curated := directToolNamesFor(FullDomainScope)
	addedName, missingName := "", ""
	for _, n := range curated {
		if n == "auth_status" {
			addedName = n
		}
		if n == "vault_create" {
			missingName = n
		}
	}
	require.NotEmpty(t, addedName, "full-surface curated set must include auth_status")
	require.NotEmpty(t, missingName, "full-surface curated set must include vault_create")

	catalog := NewToolCatalog()
	catalog.DomainScope = FullDomainScope
	catalog.Strategy = ListingProgressive
	// Simulate a gated/incomplete assembly: auth_status and websites_get
	// materialize, but vault_create does not (e.g. vault deps incomplete).
	catalog.Add(&model.ToolEntry{Name: "auth_status", DirectVisible: true})
	catalog.Add(&model.ToolEntry{Name: "websites_get", DirectVisible: true})

	card := NewServerCard(catalog)
	names := serverCardNames(card.Tools())
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	// A curated name that never materialized must NOT be advertised.
	require.False(t, set[missingName], "missing curated %q must not appear on the progressive card", missingName)
	// Materialized curated entries are advertised.
	require.True(t, set["auth_status"], "materialized curated auth_status must appear")
	require.True(t, set["websites_get"], "materialized curated websites_get must appear")
	// Meta tools are always part of the progressive direct surface.
	for _, m := range metaToolNames {
		require.True(t, set[m], "meta tool %q must appear on the progressive card", m)
	}
	// Every advertised non-meta tool must be a materialized curated entry
	// (progressive never invents names absent from the catalog).
	directSet := make(map[string]bool, len(curated))
	for _, n := range curated {
		directSet[n] = true
	}
	for _, n := range names {
		if directSet[n] {
			require.True(t, set[n], "advertised curated %q must be materialized", n)
		}
	}
}

// --- Finding 3: per-server card isolation ---

func TestServerCardIsPerServerIsolated(t *testing.T) {
	catProg := buildCardTestCatalog(t, FullDomainScope, false, ListingProgressive, true)
	catFlat := buildCardTestCatalog(t, FullDomainScope, false, ListingFlat, false)
	progCard := NewServerCard(catProg)
	flatCard := NewServerCard(catFlat)

	progBefore := serverCardNames(progCard.Tools())
	flatBefore := serverCardNames(flatCard.Tools())

	// Simulate building another server AFTER both cards were captured: mutate the
	// mutable construction-time globals. A correctly captured ServerCard must be
	// unaffected (its state was frozen at construction).
	SetListingStrategy(ListingFlat)
	SetIncludeMetaOnFlat(true)
	SetDomainScope(HostedDomainScope)

	require.Equal(t, progBefore, serverCardNames(progCard.Tools()),
		"progressive card must not change when globals mutate after capture")
	require.Equal(t, flatBefore, serverCardNames(flatCard.Tools()),
		"flat card must not change when globals mutate after capture")

	// The two cards must reflect their OWN strategies: progressive advertises the
	// meta-tools (curated+meta, smaller); flat (IncludeMetaOnFlat=false here)
	// advertises the whole direct surface without meta tools (larger).
	require.Less(t, len(progBefore), len(flatBefore), "flat card should advertise more direct tools than progressive")
	require.Contains(t, progBefore, "search_tools", "progressive card advertises meta tool search_tools")
	require.NotContains(t, flatBefore, "search_tools", "flat IncludeMetaOnFlat=false card must not advertise meta tool search_tools")
	// pins_add is search-only under progressive but directly visible under flat.
	require.NotContains(t, progBefore, "pins_add", "progressive card should not advertise search-only pins_add")
	require.Contains(t, flatBefore, "pins_add", "flat card must advertise directly-visible pins_add")

	restoreConstructionGuards(t)
}

// --- Finding 4: flat card includes direct custom tools ---

func TestFlatCardIncludesDirectCustomTool(t *testing.T) {
	srv := sdk.NewServer(nil)
	catalog := NewToolCatalog()
	catalog.DomainScope = FullDomainScope
	catalog.Strategy = ListingFlat
	catalog.IncludeMetaOnFlat = false // isolate the direct-custom behavior

	// A catalog-indexed, agent-safe direct tool for a baseline.
	handler := model.ToolHandler(func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: "ok"}, nil
	})
	schema := json.RawMessage(`{"type":"object"}`)
	catalog.Add(&model.ToolEntry{
		Name:          "pins_add",
		Category:      model.CategoryCore,
		DirectVisible: true,
		InputSchema:   schema,
		Handler:       handler,
	})

	// Register upload_file through the REAL custom-tool registry as a DIRECT-only
	// tool (no catalog index) — the no-app mode that buildCatalog's card previously
	// omitted.
	reg := newServerExtensionRegistry(srv, catalog)
	reg.add(directOnly(model.ToolDescriptor{
		Name:        "upload_file",
		Category:    model.CategoryCore,
		InputSchema: schema,
		Handler:     handler,
	}))
	require.NoError(t, reg.run(), "custom-tool registry must run")

	// The registry recorded the direct-only tool for the flat card.
	require.Contains(t, catalog.DirectCustom, "upload_file",
		"registry must record direct non-indexed tools on the catalog")

	card := NewServerCard(catalog)
	names := serverCardNames(card.Tools())
	require.Contains(t, names, "upload_file", "flat card must advertise direct custom tool upload_file")
	require.Contains(t, names, "pins_add", "flat card must advertise catalog-indexed direct tool pins_add")
	require.NotContains(t, names, "search_tools", "flat IncludeMetaOnFlat=false card must not advertise meta tools")
}
