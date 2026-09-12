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
// primary-tool predicate). Flat mode omits discovery meta-tools unless a
// consumer explicitly opts in. The deployment axes are NOT part of the policy
// (they come from ServerConfig).
func TestDefaultPolicyReproducesCurrentBehavior(t *testing.T) {
	p := DefaultPolicy()

	require.Equal(t, ListingProgressive, p.Strategy, "default listing strategy is progressive")
	require.False(t, p.ResolveIncludeMetaOnFlat(), "default omits meta-tools on flat")
	require.False(t, HasOnboardingOverride(p), "default defers onboarding to the builtin primary set")
	require.NoError(t, p.Validate(), "default policy is valid")

	// Onboarding defers to the builtin predicate (no duplicated name list).
	require.True(t, IsOnboarded(p, "auth_status"))
	require.False(t, IsOnboarded(p, "websites_create"), "websites_create is curated but not an onboarding primary")
}

// TestZeroPolicyIsProgressiveAndOmitsFlatMeta pins that a zero-value listing
// policy remains progressive and flat mode omits discovery meta-tools unless
// a consumer explicitly opts in.
func TestZeroPolicyIsProgressiveAndOmitsFlatMeta(t *testing.T) {
	p := ListingPolicy{} // all zero
	require.Equal(t, ListingProgressive, p.Strategy, "zero strategy is progressive")
	require.False(t, p.ResolveIncludeMetaOnFlat(), "unset IncludeMetaOnFlat must omit flat meta-tools")
	require.NoError(t, p.Validate())

	// An omitted IncludeMetaOnFlat omits meta-tools in flat mode.
	pFlat := ListingPolicy{Strategy: ListingFlat}
	require.False(t, pFlat.ResolveIncludeMetaOnFlat(), "flat policy with omitted IncludeMetaOnFlat must omit meta-tools")

	// Single-source regression: buildCatalogConfig delegates resolution to the
	// same policy method instead of keeping a second default.
	cfg := &buildCatalogConfig{}
	require.False(t, cfg.resolveIncludeMetaOnFlat(),
		"unset config includeMetaOnFlat must omit flat meta-tools")
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
