package mcp

// This file owns the Pinner product/assembly listing policy. It separates the
// three orthogonal axes — deployment/entitlement surface, listing
// strategy, and the direct + onboarding selection — into one
// construction-time value.
//
// Ownership boundary (Step 2 exit criteria): these types are Pinner
// product/assembly-owned and live in the Pinner consumer (this module), NOT in
// canimcp, mcpplane, or mcpforge. canimcp still answers only "what can the
// connected client do"; it carries no Hosted/DomainScope/strategy/curation concept.
//
// Because the pinned `go.lumeweb.com/pinner` module is a frozen dependency
// (no go.mod/go.sum change wanted for this step), the strategy/curation
// policy is wrapped here in the consumer rather than upstream. The only
// place we read the module is mcp.DirectToolNames (the authoritative
// direct name list), so there is no second direct copy in this repo
// and no dependency bump. Re-homing these types into go.lumeweb.com/pinner/assembly
// is deferred: it would require a pseudo-version bump + peer-repo change, which
// this step deliberately avoids.

import "fmt"

// ToolListingStrategy names the tools/list materialization policy. It is a
// Pinner product choice, encoded nowhere in canimcp; the zero value is
// progressive (the current behavior for every profile/surface).
type ToolListingStrategy uint8

const (
	// ListingProgressive materializes only the direct set plus the
	// progressive-disclosure meta-tools on tools/list; the full catalog stays
	// reachable via search_tools → describe_tool → invoke_*. This is the
	// default and the current behavior for both full and hosted surfaces.
	ListingProgressive ToolListingStrategy = iota
	// ListingFlat materializes every enabled op as a direct tool on tools/list
	// (optional meta-tools gated by IncludeMetaOnFlat). Under flat
	// the ordering metadata/selection metadata only, not a gate.
	// Materialization of flat is Step 3; this step only defines the value and
	// preserves current progressive behavior.
	ListingFlat
)

// String returns a human-readable name for the strategy. Unknown values fall
// back to a generic "<strategy N>" so formatting never panics.
func (s ToolListingStrategy) String() string {
	switch s {
	case ListingProgressive:
		return "progressive"
	case ListingFlat:
		return "flat"
	}
	return fmt.Sprintf("<strategy %d>", uint8(s))
}

// Valid reports whether s is a supported listing strategy. It is the
// single gate for rejecting unsupported/unknown policy values at construction.
func (s ToolListingStrategy) Valid() bool {
	switch s {
	case ListingProgressive, ListingFlat:
		return true
	}
	return false
}

// ListingPolicy is the server-construction input that selects listing
// behavior (the listing policy for a server). It carries ONLY the listing
// axes: the tools/list materialization strategy, the meta-on-flat switch, and
// the onboarding (direct) selection. It deliberately does NOT carry
// the deployment axes (DomainScope/Hosted): those live on ServerConfig as the
// single deployment seam, so a partial listing policy can never overwrite a
// deployment surface or hosted flag (see the withPolicy doc — the listing
// policy and the deployment axes are separate, non-overlapping construction
// inputs).
type ListingPolicy struct {
	// Strategy is the tools/list materialization strategy. The zero value
	// (ListingProgressive) is the default and current behavior.
	Strategy ToolListingStrategy
	// IncludeMetaOnFlat, when Strategy == ListingFlat, keeps the
	// progressive-disclosure meta-tools on tools/list alongside the direct set.
	// It is inert under ListingProgressive.
	//
	// It is a *bool so "unset" is representable separately from "explicitly
	// false": a nil pointer (the zero value / omitted field) means the SAFE
	// default — flat keeps the discovery meta-tools so the gated entries
	// (admin/wizard/interactive) that flat deliberately excludes from direct
	// registration stay reachable via search_tools/describe_tool/invoke_* (see
	// ResolveIncludeMetaOnFlat and DefaultPolicy). A non-nil pointer to true
	// keeps them; a non-nil pointer to false is the explicit opt-in that hides
	// those gated operations behind the direct surface entirely.
	IncludeMetaOnFlat *bool
	// Onboarding is an optional "start here" recommendation override. When
	// empty, IsOnboarded defers to the builtin primary-tool predicate
	// (isPrimaryTool) so the onboarding names are never duplicated in policy.
	// When non-empty it is a membership set independent of direct
	// status (onboarding guidance is orthogonal to listing).
	Onboarding []string
}

// DefaultPolicy returns the listing policy that reproduces current behavior:
// progressive listing and no onboarding override (defer to isPrimaryTool).
// It is a LISTING policy only — the deployment surface/hosted are separate
// inputs on ServerConfig, so this value carries no surface/hosted.
//
// IncludeMetaOnFlat defaults to TRUE as the safe invariant for any server that
// opts into ListingFlat: flat keeps the discovery meta-tools on tools/list by
// default so the gated catalog entries flat excludes from direct registration
// (admin, wizard-category, interactive) remain searchable and hand-off-capable
// through the invoke dispatchers. Excluding them (IncludeMetaOnFlat=false) is a
// deliberate override that intentionally hides gated operations from the MCP
// channel. The safest default also holds for a nil IncludeMetaOnFlat (see
// ResolveIncludeMetaOnFlat), so even a partially-specified policy keeps the
// meta tools.
func DefaultPolicy() ListingPolicy {
	keepMeta := true // safe default: flat keeps the discovery meta-tools
	return ListingPolicy{
		Strategy:          ListingProgressive, // every profile today
		IncludeMetaOnFlat: &keepMeta,
	}
}

// ResolveIncludeMetaOnFlat returns the effective meta-on-flat setting for the
// policy: TRUE when IncludeMetaOnFlat is nil (unset) — the safe invariant that
// a flat server keeps the discovery meta-tools on tools/list by default, so
// the gated entries flat excludes from direct registration stay reachable —
// and the explicit value otherwise. A nil/unset value therefore can never
// silently hide the gated operations; only an explicit false opt-in does.
// This method is the SINGLE resolution of the meta-on-flat default:
// buildCatalogConfig.resolveIncludeMetaOnFlat (catalogdeps.go) delegates here
// so the catalog-capture path cannot drift from the policy value.
func (p ListingPolicy) ResolveIncludeMetaOnFlat() bool {
	if p.IncludeMetaOnFlat == nil {
		return true
	}
	return *p.IncludeMetaOnFlat
}

// boolPtr is a test/construction convenience for building an explicit
// *bool IncludeMetaOnFlat value.
func boolPtr(b bool) *bool {
	return &b
}

// Validate reports whether the policy holds supported values. Currently this
// rejects an unknown/unsupported listing strategy. It is the construction-time
// gate so an unsupported policy value fails loudly instead of silently falling
// back.
func (p ListingPolicy) Validate() error {
	if !p.Strategy.Valid() {
		return fmt.Errorf("mcp: unsupported listing strategy %v", p.Strategy)
	}
	return nil
}

// IsOnboarded reports whether name belongs to the onboarding recommendation
// set for the policy. It delegates to the ONE shared override-evaluation
// helper (onboardingPredicate) — the same helper ToolCatalog.Onboarding
// consults — so the policy-level and catalog-level semantics can never drift:
// with no Onboarding override both defer to the builtin primary-tool predicate
// (isPrimaryTool) so the 13-name set is never copied into policy; an override
// replaces it. Onboarding is independent of DirectVisible: a recommended tool
// may be direct, searchable, or both, but never hidden.
func (p ListingPolicy) IsOnboarded(name string) bool {
	return onboardingPredicate(p.Onboarding)(name)
}

// HasOnboardingOverride reports whether the policy supplies an explicit
// "start here" set rather than deferring to the builtin primary-tool predicate.
func (p ListingPolicy) HasOnboardingOverride() bool {
	return len(p.Onboarding) > 0
}

// DEPRECATED test/legacy-compatibility seam. The listing
// strategy and meta-on-flat values below were once the read seam for
// materialization (stampDirectTools, RegisterOfficialMetaTools), server-instruction
// selection, and server-card derivation. That is no longer true:
// the ToolCatalog captured by buildCatalog (Strategy / IncludeMetaOnFlat, read
// via listingStrategy()/metaOnFlat()) is the SOLE production source of listing
// policy, and production code reads NONE of this state. The globals are
// retained only so older tests (and the agent-guide race tests that simulate a
// legacy reassembly's global writes by hand) keep compiling and can keep
// asserting that mutating them cannot alter server construction or
// materialization outcomes. New tests must drive listing policy through
// ListingPolicy / withPolicy / the catalog fields, never here.
var strategyVar = ListingProgressive

// SetListingStrategy records the listing strategy in the deprecated
// compatibility global. Production construction NEVER calls this (buildCatalog
// captures the policy on the returned ToolCatalog instead); only legacy tests
// do, to prove that the value is inert.
func SetListingStrategy(s ToolListingStrategy) {
	strategyVar = s
}

// activeStrategy returns the listing strategy recorded in the deprecated
// compatibility global, normalizing an invalid value to the default
// progressive listing. Production code no longer calls this; only legacy
// tests do (to pin the shim's own normalization behavior).
func activeStrategy() ToolListingStrategy {
	if strategyVar.Valid() {
		return strategyVar
	}
	return ListingProgressive
}

// includeMetaOnFlatVar is the deprecated compatibility counterpart to
// strategyVar. It defaults to true — the SAFE invariant a flat server keeps
// the discovery meta-tools on tools/list by default so the gated entries flat
// excludes from direct registration (admin, wizard-category, interactive) stay
// searchable/hand-off-capable via search_tools -> describe_tool -> invoke_*. —
// but production never reads this value any more: the per-catalog
// IncludeMetaOnFlat (captured by buildCatalog with the same safe-default
// resolution) is the sole production source.
var includeMetaOnFlatVar = true

// SetIncludeMetaOnFlat records whether meta tools stay on tools/list under a
// flat listing strategy in the deprecated compatibility global. Production
// construction NEVER calls this (buildCatalog resolves the safe default into
// the per-catalog IncludeMetaOnFlat instead); only legacy tests do.
func SetIncludeMetaOnFlat(include bool) {
	includeMetaOnFlatVar = include
}

// activeIncludeMetaOnFlat returns the value recorded in the deprecated
// compatibility global. Production code no longer calls this; only legacy
// tests do.
func activeIncludeMetaOnFlat() bool {
	return includeMetaOnFlatVar
}
