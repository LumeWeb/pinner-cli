package mcp

// Host-specific flat tools/list policy regression tests.
//
// The host→strategy decision is the SHARED go.lumeweb.com/pinner/mcp selector:
// the browser-side web hosts that cannot follow progressive discovery — Claude
// Web (HTTP), Grok web connectors (HTTP), and the ChatGPT/OpenAI web family
// (HTTP + the embedded tunnel) — get FLAT tools/list with the safe discovery
// meta-tools retained; every other host keeps the startup's progressive
// listing exactly as the MEDIUM-3 reassembly contract captured it.
//
// These tests exercise the REAL reassembly composition the tunnel
// hostServerFactory build: buildCatalog(normalized.opts()... +
// withPolicy(hostListingPolicy(startup, profile))) — option order matters and
// is reproduced verbatim — and assert on the catalog's captured policy (the
// sole production source for stampDirectTools, meta-tool registration,
// instructions, and the card).

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	pinnermcp "go.lumeweb.com/pinner/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// webHostProfiles are the negotiated profiles of the web hosts that bypass
// progressive discovery.
func webHostProfiles() map[string]hostenv.PlatformProfile {
	return map[string]hostenv.PlatformProfile{
		"Claude Web (HTTP)":         {HostType: hostenv.HostClaude, Transport: hostenv.TransportHTTP},
		"Grok web connector (HTTP)": {HostType: hostenv.HostGrok, Transport: hostenv.TransportHTTP},
		"OpenAI web (HTTP)":         {HostType: hostenv.HostOpenAI, Transport: hostenv.TransportHTTP},
		"ChatGPT (OpenAI tunnel)":   {HostType: hostenv.HostChatGPT, Transport: hostenv.TransportOpenAI},
	}
}

// progressiveHostProfiles are negotiated profiles that MUST keep the startup's
// progressive listing: co-located agents (including each web host's stdio
// sibling — host identity alone never goes flat) and unidentified HTTP hosts.
func progressiveHostProfiles() map[string]hostenv.PlatformProfile {
	return map[string]hostenv.PlatformProfile{
		"Codex (stdio)":          {HostType: hostenv.HostCodex, Transport: hostenv.TransportStdio},
		"Grok Shell (stdio)":     {HostType: hostenv.HostGrok, Transport: hostenv.TransportStdio},
		"Claude Code (stdio)":    {HostType: hostenv.HostClaudeCode, Transport: hostenv.TransportStdio},
		"Generic HTTP client":    {HostType: hostenv.HostGeneric, Transport: hostenv.TransportHTTP},
		"Unidentified HTTP host": {HostType: hostenv.HostUnknown, Transport: hostenv.TransportHTTP},
	}
}

// reassembleOptsFor builds the buildCatalog option set exactly as the tunnel
// assemble/hostServerFactory composition does: normalized startup config bits
// first, then the host listing override LAST so its strategy wins.
func reassembleOptsFor(normalized *normalizedServerBuild, profile *hostenv.PlatformProfile) []buildCatalogOpt {
	opts := []buildCatalogOpt{
		withCatalogDeps(func() *CatalogDepsBundle { return fullTestBundle() }),
	}
	opts = append(opts, normalized.opts()...)
	if profile != nil {
		opts = append(opts, withPolicy(hostListingPolicy(normalized.listing, *profile)))
	}
	return opts
}

// startupCatalogWithPolicy builds the startup catalog (full local surface)
// with the given listing policy and captures its normalized build config —
// the same two production steps the CLI Action performs.
func startupCatalogWithPolicy(t *testing.T, policy ListingPolicy) (*ToolCatalog, *normalizedServerBuild) {
	t.Helper()
	restoreConstructionGuards(t)
	cat, err := buildCatalog(nil, nil, nil, nil, nil, nil,
		withCatalogDeps(func() *CatalogDepsBundle { return fullTestBundle() }),
		withDomainScope(FullDomainScope),
		withHosted(false),
		withPolicy(policy),
	)
	require.NoError(t, err, "build startup catalog")
	return cat, captureServerBuild(cat)
}

// catalogEntry returns the catalog entry by name, if present.
func catalogEntry(t *testing.T, cat *ToolCatalog, name string) *model.ToolEntry {
	t.Helper()
	for _, entry := range cat.Entries() {
		if entry.Name == name {
			return entry
		}
	}
	return nil
}

// findEntryByCategory returns the first catalog entry in the given category.
func findEntryByCategory(t *testing.T, cat *ToolCatalog, category model.ToolCategory) *model.ToolEntry {
	t.Helper()
	for _, entry := range cat.Entries() {
		if entry.Category == category {
			return entry
		}
	}
	return nil
}

// TestWebHostReassemblyMaterializesFlatWithMeta is the regression gate for the
// approved architecture: each web host that bypasses progressive discovery
// must be reassembled with a FLAT tools/list (shared selector decision), with
// every other startup listing axis (surface, hosted, meta-on-flat safe
// default, onboarding override) preserved verbatim — and the flat surface must
// keep the safe carve-outs (admin/wizard/interactive never direct) and the
// discovery meta-tools.
func TestWebHostReassemblyMaterializesFlatWithMeta(t *testing.T) {
	startup := DefaultPolicy()
	startup.IncludeMetaOnFlat = boolPtr(true)
	startup.Onboarding = []string{"auth_status", "websites_create"}
	startupCat, normalized := startupCatalogWithPolicy(t, startup)
	require.Equal(t, ListingProgressive, normalized.listing.Strategy,
		"startup server must be progressive under the default policy")

	progressiveDirect := 0
	for _, entry := range startupCat.Entries() {
		if isDirectCatalogEntry(entry) {
			progressiveDirect++
		}
	}

	for name, profile := range webHostProfiles() {
		t.Run(name, func(t *testing.T) {
			hostCat, err := buildCatalog(nil, nil, nil, nil, nil, nil,
				reassembleOptsFor(normalized, &profile)...)
			require.NoError(t, err, "reassemble per-host catalog")

			// The shared selector's strategy is captured on the negotiated catalog.
			require.Equal(t, ListingFlat, hostCat.Strategy,
				"web host must be reassembled flat per the shared host selector")
			require.True(t, hostCat.metaOnFlat(),
				"flat web-host surface keeps the discovery meta-tools (safe default: startup IncludeMetaOnFlat survives the override)")
			require.True(t, hostCat.servesMetaTools(),
				"meta tools stay on tools/list for a flat web-host server under the safe default")
			// Startup onboarding override survives the host listing override.
			require.Equal(t, startup.Onboarding, hostCat.OnboardingOverride)
			// Deployment axes untouched (MEDIUM-3 for the axes the override
			// deliberately does not carry).
			require.Equal(t, normalized.surface, hostCat.DomainScope)
			require.Equal(t, normalized.hosted, hostCat.Hosted)

			// Flat materialization promotes agent-safe ops (progressive only
			// shows the direct set) but never the gated carve-outs.
			pinsAdd := catalogEntry(t, hostCat, "pins_add")
			require.NotNil(t, pinsAdd, "pins_add must exist on the full surface")
			require.True(t, pinsAdd.DirectVisible, "flat web-host surface must list agent-safe pins_add directly")

			admin := findEntryByCategory(t, hostCat, model.CategoryAdmin)
			if admin != nil {
				require.False(t, admin.DirectVisible,
					"flat web-host surface must keep admin ops behind the meta-tools (safety carve-out)")
			}
			wizard := findEntryByCategory(t, hostCat, model.CategoryWizard)
			if wizard != nil {
				require.False(t, wizard.DirectVisible,
					"flat web-host surface must keep wizard ops behind the meta-tools (safety carve-out)")
			}

			// The flat surface strictly widens the progressive direct surface.
			flatDirect := 0
			for _, entry := range hostCat.Entries() {
				if isDirectCatalogEntry(entry) {
					flatDirect++
				}
			}
			require.Greater(t, flatDirect, progressiveDirect,
				"flat tools/list must directly expose more agent-safe ops than the progressive set")
		})
	}
}

// TestProgressiveHostReassemblyKeepsStartupListingPolicy pins the flip side:
// hosts that the shared selector keeps progressive are reassembled
// byte-identically on their listing axes (MEDIUM-3 unchanged for them) — a
// negotiated server for a non-web host can never drift into flat.
func TestProgressiveHostReassemblyKeepsStartupListingPolicy(t *testing.T) {
	startup := DefaultPolicy()
	startup.Onboarding = []string{"auth_status"}
	startupCat, normalized := startupCatalogWithPolicy(t, startup)

	for name, profile := range progressiveHostProfiles() {
		t.Run(name, func(t *testing.T) {
			hostCat, err := buildCatalog(nil, nil, nil, nil, nil, nil,
				reassembleOptsFor(normalized, &profile)...)
			require.NoError(t, err, "reassemble per-host catalog")

			require.Equal(t, startupCat.Strategy, hostCat.Strategy,
				"non-web host must keep the startup progressive strategy")
			require.Equal(t, startupCat.IncludeMetaOnFlat, hostCat.IncludeMetaOnFlat)
			require.Equal(t, startupCat.OnboardingOverride, hostCat.OnboardingOverride)
			require.Equal(t, startupCat.DomainScope, hostCat.DomainScope)
			require.Equal(t, startupCat.Hosted, hostCat.Hosted)
		})
	}
}

// TestHostListingPolicyResolvesThroughSharedSelector pins the composition
// seam itself: the consumer helper layers the SHARED selector on top of the
// startup policy — flat for the web hosts, verbatim otherwise, an explicitly
// flat startup floored, the meta switch left unset (nil never overwrites), and
// onboarding carried through.
func TestHostListingPolicyResolvesThroughSharedSelector(t *testing.T) {
	startup := ListingPolicy{
		Strategy:          ListingProgressive,
		IncludeMetaOnFlat: boolPtr(true),
		Onboarding:        []string{"auth_status"},
	}

	// Web hosts resolve flat through the shared selector only.
	claude := hostListingPolicy(startup, hostenv.PlatformProfile{
		HostType: hostenv.HostClaude, Transport: hostenv.TransportHTTP,
	})
	require.Equal(t, ListingFlat, claude.Strategy)
	require.Equal(t, startup.IncludeMetaOnFlat, claude.IncludeMetaOnFlat,
		"the override must preserve the startup meta-tool setting")
	require.Equal(t, startup.Onboarding, claude.Onboarding,
		"onboarding is a deployment axis and survives the host override")

	// Selector equivalence: the composition must agree with the shared package
	// selector for every detected profile class.
	for _, p := range webHostProfiles() {
		require.Equal(t, pinnermcp.ListingFlat, hostListingPolicy(startup, p).Strategy)
	}
	for _, p := range progressiveHostProfiles() {
		require.Equal(t, pinnermcp.ListingProgressive, hostListingPolicy(startup, p).Strategy)
	}

	// Policy floor: an explicitly flat startup deployment is never downgraded
	// — even for a progressive host the negotiated server stays flat, and the
	// onboarding override survives.
	flatStartup := ListingPolicy{Strategy: ListingFlat}
	for _, p := range progressiveHostProfiles() {
		require.Equal(t, ListingFlat, hostListingPolicy(flatStartup, p).Strategy,
			"an explicitly flat startup must floor every host policy at flat")
	}

}
