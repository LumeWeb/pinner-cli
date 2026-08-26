package toolforge

import (
	"encoding/json"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
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
