package toolforge

import (
	"encoding/json"

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
	target := resolveTarget(targets, profile)
	if target == nil || !target.Visible {
		return "", false
	}
	if target.DescFunc != nil {
		// The target's DescFunc receives the SDK-neutral profile;
		// resolve the CLI-platform view for the module-local resolver.
		return target.DescFunc(profile.Shared()), true
	}
	return target.Description, true
}

// ResolveInputSchema finds the best-matching model.ToolTarget's input schema
// for a platform profile, using the same resolution rules as
// ResolveDescription.
func ResolveInputSchema(targets []model.ToolTarget, profile hostenv.PlatformProfile) (json.RawMessage, bool) {
	target := resolveTarget(targets, profile)
	if target == nil || !target.Visible {
		return nil, false
	}
	return target.InputSchema, true
}

// DescResolver adapts a CLI, PlatformProfile-based description resolver (a
// DescBuilder.Resolve method) to the SDK-neutral model.ToolTarget.DescFunc
// signature, which mcpplane/model now types as func(model.Profile) string.
// At resolution time the shared model profile is reconstructed into the CLI
// PlatformProfile view (hostenv.FromShared) and handed to the resolver.
//
// SHIM NOTE: the reconstructed profile has a zero Surface because the
// SDK-neutral profile cannot carry the CLI-only Surface field. The resolvers
// wrapped here (the toolforge DescBuilders and other description composers)
// gate only on features, transports and hosts — never on Surface, which is a
// server-construction-time property enforced at registration, not at
// per-request description resolution. A resolver that gated on Surface would
// silently see "full surface" here; keep such resolvers out of ToolTarget
// DescFuncs.
func DescResolver(resolve func(hostenv.PlatformProfile) string) func(model.Profile) string {
	return func(sp model.Profile) string {
		return resolve(hostenv.FromShared(sp))
	}
}
