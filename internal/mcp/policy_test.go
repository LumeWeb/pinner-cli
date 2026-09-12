package mcp

// Focused tests for the Pinner-owned listing policy: defaults,
// surface independence, the curated/direct + onboarding axes, and rejection of
// unsupported/unknown policy values. These tests pin that the strategy is a
// construction-time Pinner product choice (absent from canimcp) and that the
// default reproduces current progressive behavior for both full and hosted
// surfaces.
//
// The policy is a LISTING policy only: the deployment axes (DomainScope/Hosted)
// live on ServerConfig, never in the policy, so a partial listing policy can
// never overwrite them (MEDIUM-2).

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDefaultPolicyReproducesCurrentBehavior pins the invariant default:
// progressive listing and no onboarding override (deferring to the builtin
// primary-tool predicate). IncludeMetaOnFlat defaults to TRUE as the safe
// invariant for any server that opts into flat, so gated admin/wizard/
// interactive catalog entries stay reachable through the discovery meta-tools.
// The deployment axes are NOT part of the policy (they come from ServerConfig).
func TestDefaultPolicyReproducesCurrentBehavior(t *testing.T) {
	p := DefaultPolicy()

	require.Equal(t, ListingProgressive, p.Strategy, "default listing strategy is progressive")
	require.True(t, p.ResolveIncludeMetaOnFlat(), "default keeps meta-tools on flat (safe default: gated ops stay reachable)")
	require.False(t, HasOnboardingOverride(p), "default defers onboarding to the builtin primary set")
	require.NoError(t, p.Validate(), "default policy is valid")

	// Onboarding defers to the builtin predicate (no duplicated name list).
	require.True(t, IsOnboarded(p, "auth_status"))
	require.False(t, IsOnboarded(p, "websites_create"), "websites_create is curated but not an onboarding primary")
}

// TestZeroPolicyIsProgressiveAndKeepsMeta pins that a zero-value listing
// policy (all fields unset) behaves as progressive with the safe meta-on-flat
// default: an omitted IncludeMetaOnFlat must never silently hide the gated ops
// (MEDIUM-1).
func TestZeroPolicyIsProgressiveAndKeepsMeta(t *testing.T) {
	p := ListingPolicy{} // all zero
	require.Equal(t, ListingProgressive, p.Strategy, "zero strategy is progressive")
	require.True(t, p.ResolveIncludeMetaOnFlat(), "unset IncludeMetaOnFlat must resolve to the safe default true")
	require.NoError(t, p.Validate())

	// The flat strategy with an OMITTED IncludeMetaOnFlat keeps meta tools too
	// (the MEDIUM-1 bug: it used to behave as false and hide them).
	pFlat := ListingPolicy{Strategy: ListingFlat}
	require.True(t, pFlat.ResolveIncludeMetaOnFlat(), "flat policy with omitted IncludeMetaOnFlat must keep meta tools")

	// Single-source regression: buildCatalogConfig must delegate the nil (and
	// explicit) resolution to the SAME policy method instead of keeping a second
	// copy of the safe default that could drift.
	cfg := &buildCatalogConfig{}
	require.True(t, cfg.resolveIncludeMetaOnFlat(),
		"unset config includeMetaOnFlat must resolve through the policy default (true)")
	cfg.includeMetaOnFlat = boolPtr(false)
	require.False(t, cfg.resolveIncludeMetaOnFlat(),
		"explicit false must round-trip through the single resolution")
	cfg.includeMetaOnFlat = boolPtr(true)
	require.True(t, cfg.resolveIncludeMetaOnFlat(), "explicit true must round-trip through the single resolution")
}

// TestPolicyRejectsUnsupportedStrategy pins that an unknown/unsupported
// strategy value fails Validate loudly rather than silently falling back.
func TestPolicyRejectsUnsupportedStrategy(t *testing.T) {
	p := DefaultPolicy()
	p.Strategy = ToolListingStrategy(99)
	require.Error(t, p.Validate(), "unsupported strategy must fail Validate")

	for _, s := range []ToolListingStrategy{ListingProgressive, ListingFlat} {
		require.True(t, s.Valid(), "supported strategy %v must be valid", s)
		require.Equal(t, "progressive", ListingProgressive.String())
		require.Equal(t, "flat", ListingFlat.String())
	}
	require.False(t, ToolListingStrategy(99).Valid(), "unsupported strategy must not be valid")

	// SetListingStrategy records construction-time policy; activeStrategy
	// normalizes an invalid value back to progressive instead of serving it.
	SetListingStrategy(ListingFlat)
	require.Equal(t, ListingFlat, activeStrategy())
	SetListingStrategy(ToolListingStrategy(42))
	require.Equal(t, ListingProgressive, activeStrategy(), "invalid strategy normalizes to progressive")
	SetListingStrategy(ListingProgressive)
	require.Equal(t, ListingProgressive, activeStrategy())
}

// TestOnboardingOverrideIndependentOfDirect pins that an explicit onboarding
// override replaces the builtin predicate WITHOUT touching the curated/direct
// set — the guidance axis is independent of the listing axis.
func TestOnboardingOverrideIndependentOfDirect(t *testing.T) {
	p := DefaultPolicy()
	p.Onboarding = []string{"pins_add", "auth_status"} // override "start here"

	require.True(t, HasOnboardingOverride(p))
	require.True(t, IsOnboarded(p, "pins_add"))
	require.True(t, IsOnboarded(p, "auth_status"))
	require.False(t, IsOnboarded(p, "agent_guide"), "override drops the builtin agent_guide from onboarding")
	require.False(t, IsOnboarded(p, "pins_rm"), "override drops a builtin primary not in the override set")
}
