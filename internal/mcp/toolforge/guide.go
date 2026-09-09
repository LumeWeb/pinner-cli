// Package toolforge shim: the guide DSL (go.lumeweb.com/mcpforge) welded onto
// hostenv.PlatformProfile. The wire model (AgentGuide/GuideFlow/
// GuideDecision/GuideBranch) is aliased directly — the shapes and JSON tags
// are identical to the pre-extraction in-package types, so guide payloads
// encode unchanged. The builders are wrapped over the forgeCarrier adapter
// (see carrier.go): mcpforge is generic over a FeatureCarrier context, and
// the CLI platform profile's feature vocabulary is mcpplane/model's, so each
// builder method re-wraps feature values and hostenv predicates at the
// boundary. The pre-extraction convenience gates (StepWhenHost, WhenTransport,
// RuleWhenHosted, ...) are kept as thin shims over mcpforge's *Pred gates.
//
// Generic DSL machinery lives in mcpforge; this file carries the weld.
package toolforge

import (
	mcpforge "go.lumeweb.com/mcpforge"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// ---------------------------------------------------------------------------
// Wire model
// ---------------------------------------------------------------------------

// Wire model — identical shape and JSON tags to the pre-extraction types.
type (
	// AgentGuide is the structured payload returned by the agent_guide tool.
	AgentGuide = mcpforge.AgentGuide
	// GuideFlow describes one chained flow an agent can drive end-to-end.
	GuideFlow = mcpforge.GuideFlow
	// GuideDecision models a branching point in a flow.
	GuideDecision = mcpforge.GuideDecision
	// GuideBranch is one path through a decision.
	GuideBranch = mcpforge.GuideBranch
)

// ---------------------------------------------------------------------------
// Guide DSL
// ---------------------------------------------------------------------------

// GuideSpec is the top-level, profile-aware composition of the whole guide.
type GuideSpec struct {
	b mcpforge.GuideSpec[forgeCarrier]
}

// GuideFlowBuilder declares one flow and resolves it against a profile.
type GuideFlowBuilder struct {
	b mcpforge.GuideFlowBuilder[forgeCarrier]
}

// GuideDecisionBuilder declares a branching point in a flow.
type GuideDecisionBuilder struct {
	b mcpforge.GuideDecisionBuilder[forgeCarrier]
}

// GuideBranchBuilder declares one path through a GuideDecision.
type GuideBranchBuilder struct {
	b mcpforge.GuideBranchBuilder[forgeCarrier]
}

// Flow starts a flow with the given identifier and human title.
func Flow(name, title string) *GuideFlowBuilder {
	return &GuideFlowBuilder{b: *mcpforge.Flow[forgeCarrier](name, title)}
}

// Decision starts a branching point with the given question and branches.
func Decision(question string, branches ...*GuideBranchBuilder) *GuideDecisionBuilder {
	mb := make([]*mcpforge.GuideBranchBuilder[forgeCarrier], 0, len(branches))
	for _, br := range branches {
		mb = append(mb, br.unwrap())
	}
	return &GuideDecisionBuilder{b: *mcpforge.Decision[forgeCarrier](question, mb...)}
}

// Branch starts a new branch for the given natural-language condition.
func Branch(when string) *GuideBranchBuilder {
	return &GuideBranchBuilder{b: *mcpforge.Branch[forgeCarrier](when)}
}

// Guide starts a new guide specification.
func Guide() *GuideSpec {
	return &GuideSpec{b: *mcpforge.Guide[forgeCarrier]()}
}

func (b *GuideBranchBuilder) unwrap() *mcpforge.GuideBranchBuilder[forgeCarrier] { return &b.b }

// ---------------------------------------------------------------------------
// Flow builder
// ---------------------------------------------------------------------------

// Steps appends tool names that are always part of the ordered chain.
func (b *GuideFlowBuilder) Steps(names ...string) *GuideFlowBuilder {
	b.b.Steps(names...)
	return b
}

// StepWhen appends tool names included only when the resolved profile has feat.
func (b *GuideFlowBuilder) StepWhen(feat hostenv.Feature, names ...string) *GuideFlowBuilder {
	b.b.StepWhen(forgeFeature(feat), names...)
	return b
}

// StepUnless appends tool names included only when the resolved profile lacks feat.
func (b *GuideFlowBuilder) StepUnless(feat hostenv.Feature, names ...string) *GuideFlowBuilder {
	b.b.StepUnless(forgeFeature(feat), names...)
	return b
}

// StepWhenHost appends tool names included only when the profile's host matches h.
func (b *GuideFlowBuilder) StepWhenHost(h hostenv.HostType, names ...string) *GuideFlowBuilder {
	b.b.StepWhenPred(forgePred(hostenv.HostIs(h)), names...)
	return b
}

// StepUnlessHost appends tool names included only when the profile's host does NOT match h.
func (b *GuideFlowBuilder) StepUnlessHost(h hostenv.HostType, names ...string) *GuideFlowBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.HostIs(h)), names...)
	return b
}

// StepWhenSurface appends tool names included only when the profile's surface
// passes get (a Surface accessor).
func (b *GuideFlowBuilder) StepWhenSurface(get func(hostenv.Surface) bool, names ...string) *GuideFlowBuilder {
	b.b.StepWhenPred(forgePred(hostenv.SurfaceIs(get)), names...)
	return b
}

// StepUnlessSurface appends tool names included only when the profile's surface
// fails get.
func (b *GuideFlowBuilder) StepUnlessSurface(get func(hostenv.Surface) bool, names ...string) *GuideFlowBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.SurfaceIs(get)), names...)
	return b
}

// StepWhenHosted appends tool names included only when the deployment is hosted.
func (b *GuideFlowBuilder) StepWhenHosted(hosted bool, names ...string) *GuideFlowBuilder {
	b.b.StepWhenPred(forgePred(hostenv.HostedIs(hosted)), names...)
	return b
}

// StepUnlessHosted appends tool names included only when the deployment is not hosted.
func (b *GuideFlowBuilder) StepUnlessHosted(hosted bool, names ...string) *GuideFlowBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.HostedIs(hosted)), names...)
	return b
}

// Detail sets the flow's guidance, composed as a feature-gated DescBuilder.
func (b *GuideFlowBuilder) Detail(d DescBuilder) *GuideFlowBuilder {
	b.b.Detail(d.b)
	return b
}

// Decision attaches a branching decision instead of a flat step chain.
func (b *GuideFlowBuilder) Decision(d *GuideDecisionBuilder) *GuideFlowBuilder {
	b.b.Decision(&d.b)
	return b
}

// ---------------------------------------------------------------------------
// Branch builder
// ---------------------------------------------------------------------------

// Steps appends tool names that are always part of the ordered chain.
func (b *GuideBranchBuilder) Steps(names ...string) *GuideBranchBuilder {
	b.b.Steps(names...)
	return b
}

// StepWhen appends tool names included only when the resolved profile has feat.
func (b *GuideBranchBuilder) StepWhen(feat hostenv.Feature, names ...string) *GuideBranchBuilder {
	b.b.StepWhen(forgeFeature(feat), names...)
	return b
}

// StepUnless appends tool names included only when the resolved profile lacks feat.
func (b *GuideBranchBuilder) StepUnless(feat hostenv.Feature, names ...string) *GuideBranchBuilder {
	b.b.StepUnless(forgeFeature(feat), names...)
	return b
}

// StepWhenHost appends tool names included only when the profile's host matches h.
func (b *GuideBranchBuilder) StepWhenHost(h hostenv.HostType, names ...string) *GuideBranchBuilder {
	b.b.StepWhenPred(forgePred(hostenv.HostIs(h)), names...)
	return b
}

// StepUnlessHost appends tool names included only when the profile's host does NOT match h.
func (b *GuideBranchBuilder) StepUnlessHost(h hostenv.HostType, names ...string) *GuideBranchBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.HostIs(h)), names...)
	return b
}

// StepWhenSurface appends tool names included only when the profile's surface
// passes get (a Surface accessor).
func (b *GuideBranchBuilder) StepWhenSurface(get func(hostenv.Surface) bool, names ...string) *GuideBranchBuilder {
	b.b.StepWhenPred(forgePred(hostenv.SurfaceIs(get)), names...)
	return b
}

// StepUnlessSurface appends tool names included only when the profile's surface
// fails get.
func (b *GuideBranchBuilder) StepUnlessSurface(get func(hostenv.Surface) bool, names ...string) *GuideBranchBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.SurfaceIs(get)), names...)
	return b
}

// StepWhenHosted appends tool names included only when the deployment is hosted.
func (b *GuideBranchBuilder) StepWhenHosted(hosted bool, names ...string) *GuideBranchBuilder {
	b.b.StepWhenPred(forgePred(hostenv.HostedIs(hosted)), names...)
	return b
}

// StepUnlessHosted appends tool names included only when the deployment is not hosted.
func (b *GuideBranchBuilder) StepUnlessHosted(hosted bool, names ...string) *GuideBranchBuilder {
	b.b.StepUnlessPred(forgePred(hostenv.HostedIs(hosted)), names...)
	return b
}

// Detail sets the branch's guidance, composed as a feature-gated DescBuilder.
func (b *GuideBranchBuilder) Detail(d DescBuilder) *GuideBranchBuilder {
	b.b.Detail(d.b)
	return b
}

// WhenFeature includes the branch only when the resolved profile has feat.
func (b *GuideBranchBuilder) WhenFeature(feat hostenv.Feature) *GuideBranchBuilder {
	b.b.WhenFeature(forgeFeature(feat))
	return b
}

// UnlessFeature includes the branch only when the resolved profile lacks feat.
func (b *GuideBranchBuilder) UnlessFeature(feat hostenv.Feature) *GuideBranchBuilder {
	b.b.UnlessFeature(forgeFeature(feat))
	return b
}

// WhenHost includes the branch only when the profile's host matches h.
func (b *GuideBranchBuilder) WhenHost(h hostenv.HostType) *GuideBranchBuilder {
	b.b.WhenPred(forgePred(hostenv.HostIs(h)))
	return b
}

// UnlessHost includes the branch only when the profile's host does NOT match h.
func (b *GuideBranchBuilder) UnlessHost(h hostenv.HostType) *GuideBranchBuilder {
	b.b.UnlessPred(forgePred(hostenv.HostIs(h)))
	return b
}

// WhenSurface includes the branch only when the profile's surface passes get.
func (b *GuideBranchBuilder) WhenSurface(get func(hostenv.Surface) bool) *GuideBranchBuilder {
	b.b.WhenPred(forgePred(hostenv.SurfaceIs(get)))
	return b
}

// UnlessSurface includes the branch only when the profile's surface fails get.
func (b *GuideBranchBuilder) UnlessSurface(get func(hostenv.Surface) bool) *GuideBranchBuilder {
	b.b.UnlessPred(forgePred(hostenv.SurfaceIs(get)))
	return b
}

// WhenHosted includes the branch only when the deployment is hosted.
func (b *GuideBranchBuilder) WhenHosted(hosted bool) *GuideBranchBuilder {
	b.b.WhenPred(forgePred(hostenv.HostedIs(hosted)))
	return b
}

// UnlessHosted includes the branch only when the deployment is not hosted.
func (b *GuideBranchBuilder) UnlessHosted(hosted bool) *GuideBranchBuilder {
	b.b.UnlessPred(forgePred(hostenv.HostedIs(hosted)))
	return b
}

// WhenTransport includes the branch only when the profile's transport matches t.
func (b *GuideBranchBuilder) WhenTransport(t hostenv.TransportKind) *GuideBranchBuilder {
	b.b.WhenPred(forgePred(hostenv.TransportIs(t)))
	return b
}

// WhenPred includes the branch only when pred passes for the profile. It is
// the escape hatch for whole-branch gates no hostenv constructor expresses.
func (b *GuideBranchBuilder) WhenPred(pred hostenv.Predicate) *GuideBranchBuilder {
	b.b.WhenPred(forgePred(pred))
	return b
}

// UnlessPred includes the branch only when pred does NOT pass for the profile.
func (b *GuideBranchBuilder) UnlessPred(pred hostenv.Predicate) *GuideBranchBuilder {
	b.b.UnlessPred(forgePred(pred))
	return b
}

// Next attaches a nested decision. A nil decision is stored as absent,
// matching the pre-extraction builder (call sites pass an optional chain).
func (b *GuideBranchBuilder) Next(d *GuideDecisionBuilder) *GuideBranchBuilder {
	if d != nil {
		b.b.Next(&d.b)
	}
	return b
}

// ---------------------------------------------------------------------------
// Guide spec
// ---------------------------------------------------------------------------

// Summary sets the guide's opening orientation, composed as a DescBuilder.
func (g *GuideSpec) Summary(d DescBuilder) *GuideSpec {
	g.b.Summary(d.b)
	return g
}

// Rule adds an always-included operational rule.
func (g *GuideSpec) Rule(text string) *GuideSpec {
	g.b.Rule(text)
	return g
}

// RuleWhen adds a rule included only when the profile has feat.
func (g *GuideSpec) RuleWhen(feat hostenv.Feature, text string) *GuideSpec {
	g.b.RuleWhen(forgeFeature(feat), text)
	return g
}

// RuleUnless adds a rule included only when the profile lacks feat.
func (g *GuideSpec) RuleUnless(feat hostenv.Feature, text string) *GuideSpec {
	g.b.RuleUnless(forgeFeature(feat), text)
	return g
}

// RuleWhenHost adds a rule included only when the profile's host matches h.
func (g *GuideSpec) RuleWhenHost(h hostenv.HostType, text string) *GuideSpec {
	g.b.RuleWhenPred(forgePred(hostenv.HostIs(h)), text)
	return g
}

// RuleUnlessHost adds a rule included only when the profile's host does NOT
// match h.
func (g *GuideSpec) RuleUnlessHost(h hostenv.HostType, text string) *GuideSpec {
	g.b.RuleUnlessPred(forgePred(hostenv.HostIs(h)), text)
	return g
}

// RuleWhenPred adds a rule included only when pred passes for the profile.
func (g *GuideSpec) RuleWhenPred(pred hostenv.Predicate, text string) *GuideSpec {
	g.b.RuleWhenPred(forgePred(pred), text)
	return g
}

// RuleUnlessPred adds a rule included only when pred does NOT pass.
func (g *GuideSpec) RuleUnlessPred(pred hostenv.Predicate, text string) *GuideSpec {
	g.b.RuleUnlessPred(forgePred(pred), text)
	return g
}

// RuleWhenSurface adds a rule included only when the profile's surface passes
// get.
func (g *GuideSpec) RuleWhenSurface(get func(hostenv.Surface) bool, text string) *GuideSpec {
	g.b.RuleWhenPred(forgePred(hostenv.SurfaceIs(get)), text)
	return g
}

// RuleUnlessSurface adds a rule included only when the profile's surface fails
// get.
func (g *GuideSpec) RuleUnlessSurface(get func(hostenv.Surface) bool, text string) *GuideSpec {
	g.b.RuleUnlessPred(forgePred(hostenv.SurfaceIs(get)), text)
	return g
}

// RuleWhenHosted adds a rule included only when the deployment is hosted.
func (g *GuideSpec) RuleWhenHosted(hosted bool, text string) *GuideSpec {
	g.b.RuleWhenPred(forgePred(hostenv.HostedIs(hosted)), text)
	return g
}

// RuleUnlessHosted adds a rule included only when the deployment is not hosted.
func (g *GuideSpec) RuleUnlessHosted(hosted bool, text string) *GuideSpec {
	g.b.RuleUnlessPred(forgePred(hostenv.HostedIs(hosted)), text)
	return g
}

// Substitute installs a post-resolution text transform applied to every piece
// of rendered content.
func (g *GuideSpec) Substitute(fn func(string) string) *GuideSpec {
	g.b.Substitute(fn)
	return g
}

// Flow appends a flow to the guide.
func (g *GuideSpec) Flow(f *GuideFlowBuilder) *GuideSpec {
	g.b.Flow(&f.b)
	return g
}

// Resolve materializes the AgentGuide for the given platform profile.
func (g *GuideSpec) Resolve(p hostenv.PlatformProfile) AgentGuide {
	return g.b.Resolve(forgeCarrier{p})
}
