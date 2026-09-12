package mcp

// This file wraps the SHARED tool-listing policy. The listing types and the
// host-specific flat selector moved into the shared go.lumeweb.com/pinner/mcp
// assembly (pinnermcp), so both MCP consumers — this CLI's self-hosted
// assembly (stdio / HTTP / embedded OpenAI tunnel) and a hosted
// (Portal-embedded) assembly — resolve one policy value and one host→strategy
// decision instead of two drifting copies. This file re-exports those types
// under the in-repo vocabulary (aliases/consts keep every caller compiling
// unchanged) and keeps the Pinner-consumer-only pieces:
//
//   - the onboarding recommendation evaluation (isPrimaryTool vocabulary is
//     consumer-owned, so it never moved);
//   - the host-listing composition helper (hostListingPolicy), which layers
//     the shared host selector onto the startup policy the per-host
//     reassembly must preserve (MEDIUM-3);
//   - the deprecated construction-time compatibility globals (kept ONLY so
//     legacy regression tests can prove their inertness).
//
// Ownership boundary unchanged: the policy is Pinner product-owned. canimcp
// still answers only "what can the connected client do" and carries no
// strategy/disclosure concept — the shared selector names the canimcp host
// vocabulary but owns the decision.

import (
	"go.lumeweb.com/canimcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	pinnermcp "go.lumeweb.com/pinner/mcp"
)

// ToolListingStrategy aliases the shared strategy; String/Valid resolve in
// the shared package.
type ToolListingStrategy = pinnermcp.ToolListingStrategy

const (
	// ListingProgressive materializes only the direct set plus the
	// progressive-disclosure meta-tools on tools/list; the full catalog stays
	// reachable via search_tools → describe_tool → invoke_*. The default for
	// every host that is not a flat-listing web host.
	ListingProgressive = pinnermcp.ListingProgressive
	// ListingFlat materializes every agent-safe enabled op as a direct tool on
	// tools/list (meta-tools retained under the safe IncludeMetaOnFlat
	// default). Selected by the shared host selector for the web hosts that
	// bypass progressive discovery (Claude Web, Grok Web, ChatGPT/OpenAI web).
	ListingFlat = pinnermcp.ListingFlat
)

// ListingPolicy aliases the shared listing policy: the tools/list
// materialization strategy, the meta-on-flat switch, and the onboarding
// (direct) selection. It carries NO deployment axes (DomainScope/Hosted):
// those live on ServerConfig as the single deployment seam, so a partial
// listing policy can never overwrite a deployment surface or hosted flag
// (see withPolicy — the listing policy and the deployment axes are separate,
// non-overlapping construction inputs).
type ListingPolicy = pinnermcp.ListingPolicy

// DefaultPolicy returns the default listing policy: progressive listing and
// the safe meta-on-flat default. It is a LISTING policy only — the deployment
// surface/hosted are separate inputs on ServerConfig.
func DefaultPolicy() ListingPolicy {
	return pinnermcp.DefaultPolicy()
}

// ---
// Host-specific listing: the shared selector layered onto consumer policy.
// ---

// hostListingPolicy resolves the listing policy a negotiated per-host server
// is built with: the SHARED host selector re-resolves the strategy for the
// detected host (flat for Claude Web / Grok Web / ChatGPT web, the shared
// selector keeps every other host progressive), while the startup policy's
// listing axes survive the override — an explicitly flat startup deployment is
// never downgraded for a host, and the meta-on-flat switch is left unset so
// withPolicy keeps the startup's resolved value (nil never overwrites —
// the safe default stays in force on every flat surface). Onboarding is
// carried through verbatim: the recommendation set is a deployment decision,
// not a host capability.
//
// This is the one seam where the shared host selector enters per-host
// reassembly, so a negotiated server's strategy can never drift from the
// shared decision.
func hostListingPolicy(startup ListingPolicy, profile hostenv.PlatformProfile) ListingPolicy {
	strategy := pinnermcp.StrategyForHost(
		canimcpHostType(profile.HostType),
		canimcpTransportKind(profile.Transport),
	)
	// Policy floor: a startup deployment that already opted into flat (its
	// ServerConfig.Policy) keeps flat for every host, including progressive
	// hosts. Progressive is only ever relaxed upward to flat by the host
	// selector, never the other way.
	if startup.Strategy == pinnermcp.ListingFlat {
		strategy = pinnermcp.ListingFlat
	}
	return ListingPolicy{
		Strategy: strategy,
		// IncludeMetaOnFlat deliberately unset: withPolicy only writes it when
		// explicitly set, so the startup's resolved meta-on-flat survives the
		// host override (nil can never silently hide the discovery meta-tools).
		Onboarding: append([]string(nil), startup.Onboarding...),
	}
}

// isFlatListingHost maps a detected host profile onto the shared selector
// (convenience for callers already holding a hostenv profile).
func isFlatListingHost(profile hostenv.PlatformProfile) bool {
	return pinnermcp.IsFlatListingHost(
		canimcpHostType(profile.HostType),
		canimcpTransportKind(profile.Transport),
	)
}

// canimcpHostType converts the alias-vocabulary host onto the shared package's
// canimcp type; the wire strings are identical (typed constant conversion).
func canimcpHostType(h hostenv.HostType) canimcp.HostType {
	return canimcp.HostType(h)
}

// canimcpTransportKind converts the alias-vocabulary transport onto the shared
// package's canimcp type; the wire strings are identical.
func canimcpTransportKind(t hostenv.TransportKind) canimcp.TransportKind {
	return canimcp.TransportKind(t)
}

// ResolveIncludeMetaOnFlat / Validate on the policy, and String / Valid on
// the strategy, are the SHARED package's methods — resolved through the alias
// in exactly one definition, so the catalog-capture path
// (buildCatalogConfig.resolveIncludeMetaOnFlat in catalogdeps.go) and the
// construction gate can never drift from the host-selected policy value.

// boolPtr is a test/construction convenience for building an explicit
// *bool IncludeMetaOnFlat value.
func boolPtr(b bool) *bool {
	return &b
}

// IsOnboarded reports whether name belongs to the onboarding recommendation
// set for the policy. It delegates to the ONE shared override-evaluation
// helper (onboardingPredicate) — the same helper ToolCatalog.Onboarding
// consults — so the policy-level and catalog-level semantics can never drift:
// with no Onboarding override both defer to the builtin primary-tool predicate
// (isPrimaryTool) so the 13-name set is never copied into policy; an override
// replaces it. Onboarding is independent of DirectVisible: a recommended tool
// may be direct, searchable, or both, but never hidden.
//
// (A package-level function because ListingPolicy is now an alias of the
// shared go.lumeweb.com/pinner/mcp type — the onboarding vocabulary is
// consumer-owned, so its evaluation never moved upstream.)
func IsOnboarded(p ListingPolicy, name string) bool {
	return onboardingPredicate(p.Onboarding)(name)
}

// HasOnboardingOverride reports whether the policy supplies an explicit
// "start here" set rather than deferring to the builtin primary-tool predicate.
func HasOnboardingOverride(p ListingPolicy) bool {
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
