package mcp

// This file pins the MCP tool-surface shape. The onboarding and
// host-capability sections are characterization that froze the
// pre-policy behavior. The server-card section pins the invariant that the
// static serverCardTools list is gone and the card is derived from the live
// curated + meta surface, so the tests assert the derived card equals that live
// surface (no second hardcoded membership list can drift).
//
// It deliberately does NOT duplicate behavior already pinned elsewhere:
//   - exact curated tools/list set for full/hosted surface -> TestPinnerMcpAssemblePresentationParity
//   - meta-tool registration set (5 tools)               -> TestOfficialMetaToolsListed
//   - Search() exclusion rules (interactive/wizard/admin) -> TestSearchHidesWizardsByDefault,
//     TestSearchHidesAdminByDefault
//   - Onboarding returns only primary tools               -> TestOnboardingPrimaryOnly
//
// It fills only the gaps the implementation document needs: the derived
// server-card surface and its no-drift invariant, the bounded onboarding
// "isPrimaryTool" set (derived from its single production source in
// primary_flows.go), its separation from the curated/direct surface, and
// how host capabilities do (open_app) / do not (curated set) alter direct
// visibility.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// primaryToolNames is the onboarding "start here" set returned by Onboarding()
// for an empty/help search. It is DERIVED from the single production source
// (primaryOnboardingNames over the primary agent_guide flow table in
// primary_flows.go) instead of hard-coding a second full copy of the 13-name
// list. The derived set's bounded size, ordering, and the guide/onboarding
// cross-derivation contract are pinned by the tests below, so the source
// cannot silently drop or rename a member.
var primaryToolNames = primaryOnboardingNames()

// TestIsPrimaryToolExactSet pins the onboarding "start here" set as a bounded
// 13-name set led by agent_guide, with one representative member per primary
// flow group (auth, vault_create, vault_restore, pins). Full membership is
// guaranteed by derivation from the flow-spec table (guideFlowSpecs); the size and representative
// assertions here keep that derivation from drifting.
func TestIsPrimaryToolExactSet(t *testing.T) {
	require.Len(t, primaryToolNames, 13, "primary set must stay a bounded 13-name start-here set")
	require.Equal(t, "agent_guide", primaryToolNames[0], "agent_guide must lead the onboarding set")
	for _, name := range []string{
		"auth_sso", "auth_resume", // auth group
		"vault_create", "vault_create_resume", "vault_status", // vault_create group
		"vault_restore", "vault_restore_resume", // vault_restore group
		"pins_add", "pins_rm", // pins group
	} {
		assert.Truef(t, isPrimaryTool(name), "expected primary tool %q", name)
	}
	// No tool outside the exact set is treated as primary: a curated-but-not-
	// primary tool (website publishing), a search-only op, and an interactive
	// wizard step must all be excluded from the onboarding listing.
	for _, name := range []string{
		"websites_create", "websites_get", "vault_share_accept", // curated, NOT onboarding
		"vault_ls", "upload_file", "dns_zones_list", // search-only ops
		"websites_wizard_start", "websites_wizard_step", // interactive wizard
	} {
		assert.Falsef(t, isPrimaryTool(name), "non-primary tool %q must not be onboarded", name)
	}
}

// TestPrimaryGuideFlowsDeriveOnboardingSource pins the group/order/text
// contract between the consumers of the single declarative flow-spec source
// (guideFlowSpecs in guide_flows.go, onboarding entries): the resolved agent
// guide must keep the four primary flows in table order carrying the table's
// titles and exact ordered steps, and every step must be an onboarding member.
// The flow Detail prose is intentionally NOT pinned here (characterized by the
// agent_guide_test.go suite); this is the membership/group/order contract.
func TestPrimaryGuideFlowsDeriveOnboardingSource(t *testing.T) {
	guide := buildAgentGuideFor(&hostenv.ProfileStdioGeneric, FullDomainScope, false, nil)
	pos := map[string]int{}
	for i, f := range guide.Flows {
		pos[f.Name] = i
	}
	prev := -1
	for _, pf := range guideFlowSpecs {
		if !pf.onboarding {
			continue
		}
		idx, ok := pos[pf.name]
		require.Truef(t, ok, "primary flow %q missing from the resolved guide", pf.name)
		require.Greaterf(t, idx, prev, "primary flow %q must keep its source-table position in the guide", pf.name)
		prev = idx
		f := guide.Flows[idx]
		require.Equalf(t, pf.title, f.Title, "primary flow %q title", pf.name)
		require.Equalf(t, pf.steps, f.Steps, "primary flow %q ordered steps", pf.name)
		for _, s := range f.Steps {
			require.Truef(t, isPrimaryTool(s), "guide primary flow step %q must be an onboarding member", s)
		}
	}
}

// TestOnboardingSetSeparateFromDirectSet pins the core orthogonal-axis
// invariant: the onboarding recommendation set is INDEPENDENT of
// the curated/direct tools/list set. The two sets overlap but neither contains
// the other — pins_* are onboarded yet search-only, and website publishing +
// vault_share_accept are curated yet not onboarded.
func TestOnboardingSetSeparateFromDirectSet(t *testing.T) {
	primary := map[string]bool{}
	for _, name := range primaryToolNames {
		primary[name] = true
	}
	curated := map[string]bool{}
	for _, name := range directToolNamesFor(FullDomainScope) {
		curated[name] = true
	}

	// A recommended tool need not be directly visible (pins_* are search-only).
	for _, name := range []string{"pins_add", "pins_list", "pins_status", "pins_rm"} {
		assert.Truef(t, primary[name], "%q must be onboarded", name)
		assert.Falsef(t, curated[name], "%q must NOT be curated/direct — onboarding is independent of Direct", name)
	}
	// A curated/direct tool need not be onboarded (website publishing, share).
	for _, name := range []string{"websites_create", "websites_get", "vault_share_accept"} {
		assert.Truef(t, curated[name], "%q must be curated/direct", name)
		assert.Falsef(t, primary[name], "%q must NOT be onboarded", name)
	}
	// Neither set is a subset of the other, so the two axes are truly separate.
	assert.False(t, isSubsetMap(primary, curated), "onboarding set must not be a subset of curated/direct set")
	assert.False(t, isSubsetMap(curated, primary), "curated/direct set must not be a subset of onboarding set")
}

// TestOnboardingListingIsDirectIndependentCharacterization builds a catalog
// carrying both primary tools and curated-only/direct-only tools and pins that
// Onboarding() returns exactly the primary set regardless of DirectVisible or
// curated status. This exercises the Onboarding() output against isPrimaryTool
// (Step 0's second target) without asserting a constant against itself.
func TestOnboardingListingIsDirectIndependentCharacterization(t *testing.T) {
	c := NewToolCatalog()
	allNames := append([]string{}, primaryToolNames...)
	// Add curated/direct tools that must NOT appear in onboarding even though
	// they are directly visible on the surface.
	allNames = append(allNames, "websites_create", "websites_get", "vault_share_accept")
	for _, name := range allNames {
		c.Add(&model.ToolEntry{
			Name:        name,
			Title:       name,
			Category:    model.CategoryCore,
			InputSchema: []byte(`{"type":"object"}`),
			Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: "ok"}, nil
			},
		})
	}

	res := c.Onboarding()
	got := make(map[string]bool, len(res.Tools))
	for _, s := range res.Tools {
		got[s.Name] = true
	}
	require.Equal(t, len(primaryToolNames), res.Total, "onboarding must return exactly the primary set")
	for _, name := range primaryToolNames {
		assert.Truef(t, got[name], "primary tool %q must be listed", name)
	}
	for _, name := range []string{"websites_create", "websites_get", "vault_share_accept"} {
		assert.Falsef(t, got[name], "direct-only tool %q must not be onboarded", name)
	}
}

// serverCardNames extracts the names from a derived server-card tool list in
// order. Kept local so the invariants read clearly against the live surface.
func serverCardNames(tools []map[string]any) []string {
	names := make([]string, 0, len(tools))
	for _, m := range tools {
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}

// serverCardDescriptions extracts the description for each derived server-card
// tool keyed by name.
func serverCardDescriptions(tools []map[string]any) map[string]string {
	descs := make(map[string]string, len(tools))
	for _, m := range tools {
		if n, ok := m["name"].(string); ok {
			descs[n] = m["description"].(string)
		}
	}
	return descs
}

// TestServerCardToolsDerivedFromLiveDomainScope pins the invariant
// that replaced the old static-list characterization: the server card's tool
// list is derived from the authoritative live surface (curated set via
// directToolNamesFor(surface) + the meta-tool set), never a second hardcoded
// membership list. It asserts the card equals the live curated+meta surface in
// order for both the full and hosted surfaces, and that every derived tool
// carries a description.
func TestServerCardToolsDerivedFromLiveDomainScope(t *testing.T) {
	// Derive with EXPLICIT arguments (unmaterialized progressive: strategy
	// progressive, meta kept, no materialized catalog) rather than the
	// global-backed helper, so the assertions are independent of leaked
	// construction-time globals (LOW-5).
	for _, surface := range []DomainScope{FullDomainScope, HostedDomainScope} {
		want := append([]string{}, directToolNamesFor(surface)...)
		want = append(want, metaToolNames...)
		got := serverCardNames(deriveCardTools(surface, ListingProgressive, true, nil))
		require.Equalf(t, want, got, "server-card must equal curated+meta for surface %v", surface)

		descs := serverCardDescriptions(deriveCardTools(surface, ListingProgressive, true, nil))
		require.Len(t, descs, len(want), "every derived tool must carry a description for surface %v", surface)
		for _, n := range want {
			require.NotEmptyf(t, descs[n], "derived tool %q must carry a description on the card (surface %v)", n, surface)
		}
	}
}

// TestServerCardToolsNoStaticList pins the two concrete ways the old static
// list drifted, now enforced as invariants of the derived card. A curated tool
// the old list silently dropped must be present, and the pre-Step-1 extras
// (search-only ops and non-curated direct tools that a derive-from-curated+meta
// step must not carry) must be absent.
func TestServerCardToolsNoStaticList(t *testing.T) {
	// Explicit args (progressive, meta kept, no catalog) — independent of globals (LOW-5).
	card := serverCardNames(deriveCardTools(FullDomainScope, ListingProgressive, true, nil))
	set := map[string]bool{}
	for _, n := range card {
		set[n] = true
	}
	require.Len(t, card, len(directToolNamesFor(FullDomainScope))+len(metaToolNames))

	// Curated tools the old static serverCardTools silently dropped (drift) are
	// now present because membership comes from the live curated surface.
	for _, n := range []string{"vault_share_accept", "websites_get"} {
		assert.Truef(t, set[n], "derived card must include curated %q (old static list drifted)", n)
	}

	// Old static-card extras that are NOT part of the curated+meta surface must
	// no longer appear: the card is no longer a second mirror of search-only
	// ops or non-curated direct tools.
	for _, n := range []string{
		"pins_add", "pins_list", "pins_status", "pins_rm",
		"vault_ls", "auth_sso", "websites_list", "websites_validate", "upload_file",
	} {
		assert.Falsef(t, set[n], "derived card must NOT list %q (outside curated+meta surface)", n)
	}
}

// TestHostCapabilityDoesNotAlterDirectSet pins that host capability
// facts never decide the curated/direct surface (a host fact
// may inform policy, never be policy). stampDirectTools is host-agnostic: the exact
// same names become DirectVisible regardless of the detected host profile.
func TestHostCapabilityDoesNotAlterDirectSet(t *testing.T) {
	for _, profile := range []hostenv.PlatformProfile{
		hostenv.ProfileHTTPGeneric, // agent-only host
		hostenv.ProfileClaudeHTTP,  // GUI host (FeatMCPApps)
	} {
		c := NewToolCatalog()
		c.DomainScope = FullDomainScope
		for _, name := range directToolNamesFor(FullDomainScope) {
			c.Add(&model.ToolEntry{Name: name, Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: "ok"}, nil
			}})
		}
		stampDirectTools(c)
		for _, name := range directToolNamesFor(FullDomainScope) {
			e, ok := c.Get(name)
			require.Truef(t, ok, "%s: %q must be registered", profile.HostType, name)
			assert.Truef(t, e.DirectVisible, "%s: curated %q must be DirectVisible regardless of host capability", profile.HostType, name)
		}
	}
}

// TestOpenAppDirectVisibilityGatedOnGUICapability pins the one place a host
// capability DOES alter direct visibility: the consolidated open_app launcher
// is projected onto tools/list only when the effective host features include
// FeatMCPApps (GUI-capable); on an agent-only host it stays catalog-indexed
// (DirectVisible=false). This is the guiCapable computation used by
// registerCustomTools.
func TestOpenAppDirectVisibilityGatedOnGUICapability(t *testing.T) {
	guiProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioMCPApps,
		hostenv.ProfileClaudeHTTP,
		hostenv.ProfileOpenAIHTTP,
		hostenv.ProfileOpenAITunnel,
	}
	agentProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioGeneric,
		hostenv.ProfileHTTPGeneric,
		hostenv.ProfileGrokHTTP,
		hostenv.ProfileGrokStdio,
	}

	for _, p := range guiProfiles {
		deps := customToolDeps{hostProfile: &p}
		guiCapable := effectiveFeaturesFor(deps).Has(hostenv.FeatMCPApps)
		assert.Truef(t, guiCapable, "%s: GUI host must be guiCapable -> open_app DirectVisible", p.HostType)
	}
	for _, p := range agentProfiles {
		deps := customToolDeps{hostProfile: &p}
		guiCapable := effectiveFeaturesFor(deps).Has(hostenv.FeatMCPApps)
		assert.Falsef(t, guiCapable, "%s: agent-only host must NOT be guiCapable -> open_app search-only", p.HostType)
	}
}

// isSubsetMap reports whether every key of a is present in b.
func isSubsetMap(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
