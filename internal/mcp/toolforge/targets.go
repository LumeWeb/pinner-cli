// Package toolforge shim: the pinner-domain target constructors over
// go.lumeweb.com/mcpplane/model.ToolTarget. The constructors (Target,
// Fallback, MCPTargets, Hidden) build the model-layer target values exactly
// as before; resolution itself lives in mcpforge.ResolveTarget — the
// forgeTarget/forgeTargets adapters convert model.ToolTarget values onto
// mcpforge.Target[forgeCarrier] on the fly (feature keys are re-typed at
// the boundary because the CLI vocabulary is mcpplane/model's; see
// carrier.go).
package toolforge

import (
	"go.lumeweb.com/mcpforge"
	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// featureSet builds a FeatureSet from a variadic list of features.
func featureSet(features ...hostenv.Feature) hostenv.FeatureSet {
	fs := make(hostenv.FeatureSet, len(features))
	for _, f := range features {
		fs[f] = true
	}
	return fs
}

// Target creates a visible model.ToolTarget that requires all given features.
// Among all matching targets, the one with the most required features wins.
// Use it for transport-specific description variants:
//
//	Target("Upload a file...", hostenv.FeatFileHostInput, hostenv.FeatSourceURL)
func Target(desc string, features ...hostenv.Feature) model.ToolTarget {
	return model.ToolTarget{
		Require:     featureSet(features...),
		Visible:     true,
		Description: desc,
	}
}

// Fallback creates a visible model.ToolTarget with no feature requirements.
// It always matches (score 0), so it only wins when no specific target does.
// Every tool's target list should end with a Fallback to guarantee resolution.
//
//	Fallback("Upload a file...")
func Fallback(desc string) model.ToolTarget {
	return model.ToolTarget{
		Require:     hostenv.FeatureSet{},
		Visible:     true,
		Description: desc,
	}
}

// MCPTargets wraps a variadic list of ToolTargets into a slice, mirroring
// catalog.MCPTargets for the model-level target type. Use it when declaring a
// descriptor's MCPTargets for readability:
//
//	MCPTargets: toolforge.MCPTargets(toolforge.Fallback("Upload a file..."))
func MCPTargets(targets ...model.ToolTarget) []model.ToolTarget { return targets }

// Hidden creates an invisible model.ToolTarget that suppresses the tool entirely
// for platforms matching the given features. Useful when a tool should not be
// advertised to certain hosts.
//
//	Hidden(hostenv.FeatCoLocated)
func Hidden(features ...hostenv.Feature) model.ToolTarget {
	return model.ToolTarget{
		Require: featureSet(features...),
		Visible: false,
	}
}

// forgeTarget maps a model.ToolTarget onto the mcpforge target type so
// resolution runs through mcpforge.ResolveTarget. The security schemes are
// converted field-by-field because model.SecurityScheme and
// mcpforge.SecurityScheme are distinct named types standing for the same
// wire shape; the feature set is re-keyed onto mcpforge.Feature (see
// carrier.go). The DescFunc is adapted to receive the SDK-neutral profile
// view of the resolved carrier, matching the pre-extraction resolution
// behavior.
func forgeTarget(t model.ToolTarget) mcpforge.Target[forgeCarrier] {
	schemes := make([]mcpforge.SecurityScheme, 0, len(t.SecuritySchemes))
	for _, s := range t.SecuritySchemes {
		schemes = append(schemes, mcpforge.SecurityScheme{Type: s.Type, Scopes: s.Scopes})
	}
	ft := mcpforge.Target[forgeCarrier]{
		Require:         toForgeFeatureSet(t.Require),
		Visible:         t.Visible,
		Description:     t.Description,
		InputSchema:     t.InputSchema,
		OutputSchema:    t.OutputSchema,
		Meta:            t.Meta,
		SecuritySchemes: schemes,
		SensitiveFlags:  t.SensitiveFlags,
	}
	if t.DescFunc != nil {
		fn := t.DescFunc
		ft.DescFunc = func(c forgeCarrier) string { return fn(c.profile.Shared()) }
	}
	return ft
}

// forgeTargets maps a model target slice onto the mcpforge slice type,
// preserving declaration order (which resolution semantics depend on).
func forgeTargets(targets []model.ToolTarget) []mcpforge.Target[forgeCarrier] {
	out := make([]mcpforge.Target[forgeCarrier], 0, len(targets))
	for _, t := range targets {
		out = append(out, forgeTarget(t))
	}
	return out
}
