// Package toolforge shim: per-profile resolution over model.ToolTarget
// slices, delegated to mcpforge.ResolveTarget after conversion.
// mcpforge.ResolveTarget is the deterministic extractor of the
// pre-extraction resolveTarget: among all targets whose Require set is fully
// satisfied by the profile's features, the one with the most required
// features wins; ties are broken by declaration order (first wins).
package toolforge

import (
	"go.lumeweb.com/mcpforge"
	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// ResolveDescription finds the best-matching model.ToolTarget's description
// for a platform profile. Among all targets whose Require set is fully
// satisfied by the profile's features, the one with the most required
// features wins (ties broken by declaration order). Returns the description
// and true on match; empty string and false if no target matches or the
// target is hidden.
func ResolveDescription(targets []model.ToolTarget, profile hostenv.PlatformProfile) (string, bool) {
	matched := mcpforge.ResolveTarget(forgeTargets(targets), forgeCarrier{profile})
	if matched == nil || !matched.Visible {
		return "", false
	}
	if matched.DescFunc != nil {
		// The converted target adapts the model target's DescFunc to
		// receive the SDK-neutral profile view, exactly as the
		// pre-extraction resolver did.
		return matched.DescFunc(forgeCarrier{profile}), true
	}
	return matched.Description, true
}

// DescResolver adapts a CLI, PlatformProfile-based description resolver (a
// DescBuilder.Resolve method) to the SDK-neutral model.ToolTarget.DescFunc
// signature, which mcpplane/model now types as func(model.Profile) string.
// At resolution time the shared model profile is reconstructed into the CLI
// PlatformProfile view (hostenv.FromShared) and handed to the resolver.
//
// SHIM NOTE: the reconstructed profile has a zero DomainScope because the
// SDK-neutral profile cannot carry the CLI-only DomainScope field. The resolvers
// wrapped here (the toolforge DescBuilders and other description composers)
// gate only on features, transports and hosts — never on DomainScope, which is a
// server-construction-time property enforced at registration, not at
// per-request description resolution. A resolver that gated on DomainScope would
// silently see "full surface" here; keep such resolvers out of ToolTarget
// DescFuncs.
func DescResolver(resolve func(hostenv.PlatformProfile) string) func(model.Profile) string {
	return func(sp model.Profile) string {
		return resolve(hostenv.FromShared(sp))
	}
}
