package toolforge

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// ---------------------------------------------------------------------------
// Guide wire model
//
// These are the structured types the agent_guide tool returns. They live here
// (in the platform-DSL package) so the guide can be composed by the same DSL
// that builds schemas and descriptions, and so `mcp` only aliases them instead
// of maintaining a parallel definition.
// ---------------------------------------------------------------------------

// GuideFlow describes one chained flow an agent can drive end-to-end.
// Simple flows use Steps directly. Branching flows use Decision so the agent
// picks the correct path deterministically instead of guessing.
type GuideFlow struct {
	Name     string         `json:"name"`               // flow identifier, e.g. auth
	Title    string         `json:"title"`              // short human label
	Steps    []string       `json:"steps,omitempty"`    // ordered tools (simple flows)
	Detail   string         `json:"detail,omitempty"`   // one-line guidance
	Decision *GuideDecision `json:"decision,omitempty"` // branching flows
}

// GuideDecision models a branching point in a flow. The agent evaluates each
// Branch's When clause and follows the first match.
type GuideDecision struct {
	Question string        `json:"question"` // what to decide
	Branches []GuideBranch `json:"branches"` // ordered branches
}

// GuideBranch is one path through a decision. When is a natural-language
// condition; Steps is the ordered tool chain for that path; Detail is
// guidance; Next allows nested decisions.
type GuideBranch struct {
	When   string         `json:"when"`  // condition for this branch
	Steps  []string       `json:"steps"` // ordered tools for that path
	Detail string         `json:"detail,omitempty"`
	Next   *GuideDecision `json:"next,omitempty"` // nested decision if needed
}

// AgentGuide is the structured payload returned by the agent_guide tool.
type AgentGuide struct {
	Summary string      `json:"summary"`
	Flows   []GuideFlow `json:"flows"`
	Rules   []string    `json:"rules,omitempty"` // operational invariants
}

// ---------------------------------------------------------------------------
// Guide DSL
//
// GuideSpec, Flow and Branch compose guide content declaratively from
// feature-gated contributions — the guide twin of SchemaBuilder/DescBuilder.
// Resolving against a PlatformProfile emits only the flows/steps/branches that
// platform actually supports, so guide steps can never advertise a tool or
// source mode the resolved tool schema/surface rejects.
// ---------------------------------------------------------------------------

// gateAllows reports whether a when/unless feature pair matches the profile.
// Shared by step and branch gating so both apply feature gates identically.
func gateAllows(when, unless hostenv.Feature, p hostenv.PlatformProfile) bool {
	if when != "" && !p.Has(when) {
		return false
	}
	if unless != "" && p.Has(unless) {
		return false
	}
	return true
}

// gatedStep is one ordered contribution of tool names to a flow/branch step
// chain. Its names are emitted only when its gate matches the resolved
// profile; a zero gate always emits. A step is gated either by a feature pair
// (when/unless) or by a platform predicate (pred); pred takes precedence.
type gatedStep struct {
	when   hostenv.Feature
	unless hostenv.Feature
	pred   hostenv.Predicate
	names  []string
}

func (s gatedStep) allows(p hostenv.PlatformProfile) bool {
	if s.pred != nil {
		return s.pred(p)
	}
	return gateAllows(s.when, s.unless, p)
}

func (s gatedStep) resolve(p hostenv.PlatformProfile) []string {
	if !s.allows(p) {
		return nil
	}
	return s.names
}

// resolveSteps flattens an ordered list of gated steps into the tool chain
// active for p. Both Flow and Branch route through it so step gating is
// defined once.
func resolveSteps(steps []gatedStep, p hostenv.PlatformProfile) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.resolve(p)...)
	}
	return out
}

// addStep appends a single gated contribution to a step chain. Several build
// methods share it to avoid repeating append+return boilerplate.
func addStep(gates *[]gatedStep, g gatedStep) {
	*gates = append(*gates, g)
}

// GuideFlowBuilder declares one flow and resolves it against a profile.
type GuideFlowBuilder struct {
	name, title string
	gates       []gatedStep
	detail      DescBuilder
	hasDetail   bool
	decision    *GuideDecisionBuilder
}

// Flow starts a flow with the given identifier and human title.
func Flow(name, title string) *GuideFlowBuilder { return &GuideFlowBuilder{name: name, title: title} }

// Steps appends tool names that are always part of the ordered chain.
func (b *GuideFlowBuilder) Steps(names ...string) *GuideFlowBuilder {
	addStep(&b.gates, gatedStep{names: names})
	return b
}

// StepWhen appends tool names included only when the resolved profile has feat.
func (b *GuideFlowBuilder) StepWhen(feat hostenv.Feature, names ...string) *GuideFlowBuilder {
	addStep(&b.gates, gatedStep{when: feat, names: names})
	return b
}

// StepUnless appends tool names included only when the resolved profile lacks feat.
func (b *GuideFlowBuilder) StepUnless(feat hostenv.Feature, names ...string) *GuideFlowBuilder {
	addStep(&b.gates, gatedStep{unless: feat, names: names})
	return b
}

// StepWhenHost appends tool names included only when the profile's host matches h.
func (b *GuideFlowBuilder) StepWhenHost(h hostenv.HostType, names ...string) *GuideFlowBuilder {
	addStep(&b.gates, gatedStep{pred: hostenv.HostIs(h), names: names})
	return b
}

// StepUnlessHost appends tool names included only when the profile's host does NOT match h.
func (b *GuideFlowBuilder) StepUnlessHost(h hostenv.HostType, names ...string) *GuideFlowBuilder {
	addStep(&b.gates, gatedStep{pred: hostenv.Not(hostenv.HostIs(h)), names: names})
	return b
}

// Detail sets the flow's guidance, composed as a feature-gated DescBuilder.
func (b *GuideFlowBuilder) Detail(d DescBuilder) *GuideFlowBuilder {
	b.detail = d
	b.hasDetail = true
	return b
}

// Decision attaches a branching decision instead of a flat step chain.
func (b *GuideFlowBuilder) Decision(d *GuideDecisionBuilder) *GuideFlowBuilder {
	b.decision = d
	return b
}

func (b *GuideFlowBuilder) resolve(p hostenv.PlatformProfile, sub func(string) string) GuideFlow {
	out := GuideFlow{Name: b.name, Title: b.title, Steps: resolveSteps(b.gates, p)}
	if b.hasDetail {
		out.Detail = sub(b.detail.Resolve(p))
	}
	if b.decision != nil {
		out.Decision = b.decision.resolve(p, sub)
	}
	return out
}

// GuideDecisionBuilder declares a branching point in a flow.
type GuideDecisionBuilder struct {
	question string
	branches []*GuideBranchBuilder
}

// Decision starts a branching point with the given question and branches.
func Decision(question string, branches ...*GuideBranchBuilder) *GuideDecisionBuilder {
	return &GuideDecisionBuilder{question: question, branches: branches}
}

func (b *GuideDecisionBuilder) resolve(p hostenv.PlatformProfile, sub func(string) string) *GuideDecision {
	var out []GuideBranch
	for _, br := range b.branches {
		if rb, ok := br.resolve(p, sub); ok {
			out = append(out, rb)
		}
	}
	return &GuideDecision{Question: b.question, Branches: out}
}

// GuideBranchBuilder declares one path through a GuideDecision.
type GuideBranchBuilder struct {
	when       string
	gates      []gatedStep
	detail     DescBuilder
	hasDetail  bool
	next       *GuideDecisionBuilder
	branchWhen hostenv.Feature   // whole-branch feature gate (optional)
	branchUnls hostenv.Feature   // whole-branch feature gate (optional)
	branchPred hostenv.Predicate // whole-branch platform predicate (optional, wins over features)
}

// Branch starts a new branch for the given natural-language condition.
func Branch(when string) *GuideBranchBuilder { return &GuideBranchBuilder{when: when} }

// Steps appends tool names that are always part of the ordered chain.
func (b *GuideBranchBuilder) Steps(names ...string) *GuideBranchBuilder {
	addStep(&b.gates, gatedStep{names: names})
	return b
}

// StepWhen appends tool names included only when the resolved profile has feat.
func (b *GuideBranchBuilder) StepWhen(feat hostenv.Feature, names ...string) *GuideBranchBuilder {
	addStep(&b.gates, gatedStep{when: feat, names: names})
	return b
}

// StepUnless appends tool names included only when the resolved profile lacks feat.
func (b *GuideBranchBuilder) StepUnless(feat hostenv.Feature, names ...string) *GuideBranchBuilder {
	addStep(&b.gates, gatedStep{unless: feat, names: names})
	return b
}

// StepWhenHost appends tool names included only when the profile's host matches h.
func (b *GuideBranchBuilder) StepWhenHost(h hostenv.HostType, names ...string) *GuideBranchBuilder {
	addStep(&b.gates, gatedStep{pred: hostenv.HostIs(h), names: names})
	return b
}

// StepUnlessHost appends tool names included only when the profile's host does NOT match h.
func (b *GuideBranchBuilder) StepUnlessHost(h hostenv.HostType, names ...string) *GuideBranchBuilder {
	addStep(&b.gates, gatedStep{pred: hostenv.Not(hostenv.HostIs(h)), names: names})
	return b
}

// Detail sets the branch's guidance, composed as a feature-gated DescBuilder.
func (b *GuideBranchBuilder) Detail(d DescBuilder) *GuideBranchBuilder {
	b.detail = d
	b.hasDetail = true
	return b
}

// WhenFeature includes the branch only when the resolved profile has feat.
func (b *GuideBranchBuilder) WhenFeature(feat hostenv.Feature) *GuideBranchBuilder {
	b.branchWhen = feat
	return b
}

// UnlessFeature includes the branch only when the resolved profile lacks feat.
func (b *GuideBranchBuilder) UnlessFeature(feat hostenv.Feature) *GuideBranchBuilder {
	b.branchUnls = feat
	return b
}

// WhenHost includes the branch only when the profile's host matches h.
func (b *GuideBranchBuilder) WhenHost(h hostenv.HostType) *GuideBranchBuilder {
	b.branchPred = hostenv.HostIs(h)
	return b
}

// UnlessHost includes the branch only when the profile's host does NOT match h.
func (b *GuideBranchBuilder) UnlessHost(h hostenv.HostType) *GuideBranchBuilder {
	b.branchPred = hostenv.Not(hostenv.HostIs(h))
	return b
}

// WhenTransport includes the branch only when the profile's transport matches
// t. Use it to gate a branch on the transport (e.g. vault_put_file's url/data
// source modes, which only exist on the OpenAI tunnel) rather than on a
// capability feature that a host may declare for a DIFFERENT tool.
func (b *GuideBranchBuilder) WhenTransport(t hostenv.TransportKind) *GuideBranchBuilder {
	b.branchPred = hostenv.TransportIs(t)
	return b
}

// Next attaches a nested decision.
func (b *GuideBranchBuilder) Next(d *GuideDecisionBuilder) *GuideBranchBuilder {
	b.next = d
	return b
}

// allows reports whether the branch's whole-branch gate passes for p. A
// platform predicate wins over the feature pair when set.
func (b *GuideBranchBuilder) allows(p hostenv.PlatformProfile) bool {
	if b.branchPred != nil {
		return b.branchPred(p)
	}
	return gateAllows(b.branchWhen, b.branchUnls, p)
}

// resolve materializes the branch for p, or reports whether it is inactive
// (feature-gated off, or resolved to an empty step chain).
func (b *GuideBranchBuilder) resolve(p hostenv.PlatformProfile, sub func(string) string) (GuideBranch, bool) {
	if !b.allows(p) {
		return GuideBranch{}, false
	}
	steps := resolveSteps(b.gates, p)
	if len(steps) == 0 {
		return GuideBranch{}, false
	}
	out := GuideBranch{When: b.when, Steps: steps}
	if b.hasDetail {
		out.Detail = sub(b.detail.Resolve(p))
	}
	if b.next != nil {
		out.Next = b.next.resolve(p, sub)
	}
	return out, true
}

// gatedRule is one operational rule, optionally gated on a feature or a
// platform predicate (pred wins over the feature pair when set).
type gatedRule struct {
	text   string
	when   hostenv.Feature
	unless hostenv.Feature
	pred   hostenv.Predicate
}

// GuideSpec is the top-level, profile-aware composition of the whole guide:
// a Summary, a set of Rules, and an ordered set of Flows. Resolve against a
// PlatformProfile to produce the concrete AgentGuide.
type GuideSpec struct {
	summary    DescBuilder
	hasSummary bool
	rules      []gatedRule
	flows      []*GuideFlowBuilder
	substitute func(string) string
}

// Guide starts a new guide specification.
func Guide() *GuideSpec { return &GuideSpec{} }

// Summary sets the guide's opening orientation, composed as a DescBuilder.
func (g *GuideSpec) Summary(d DescBuilder) *GuideSpec {
	g.summary = d
	g.hasSummary = true
	return g
}

// Rule adds an always-included operational rule.
func (g *GuideSpec) Rule(text string) *GuideSpec {
	g.rules = append(g.rules, gatedRule{text: text})
	return g
}

// RuleWhen adds a rule included only when the profile has feat.
func (g *GuideSpec) RuleWhen(feat hostenv.Feature, text string) *GuideSpec {
	g.rules = append(g.rules, gatedRule{text: text, when: feat})
	return g
}

// RuleUnless adds a rule included only when the profile lacks feat.
func (g *GuideSpec) RuleUnless(feat hostenv.Feature, text string) *GuideSpec {
	g.rules = append(g.rules, gatedRule{text: text, unless: feat})
	return g
}

// RuleWhenHost adds a rule included only when the profile's host matches h.
func (g *GuideSpec) RuleWhenHost(h hostenv.HostType, text string) *GuideSpec {
	g.rules = append(g.rules, gatedRule{text: text, pred: hostenv.HostIs(h)})
	return g
}

// RuleUnlessHost adds a rule included only when the profile's host does NOT match h.
func (g *GuideSpec) RuleUnlessHost(h hostenv.HostType, text string) *GuideSpec {
	g.rules = append(g.rules, gatedRule{text: text, pred: hostenv.Not(hostenv.HostIs(h))})
	return g
}

// Substitute installs a post-resolution text transform applied to every piece
// of rendered content (summary, rules, flow/branch details). It lets platform
// tokens such as the transport source-mode list be interpolated in one place.
func (g *GuideSpec) Substitute(fn func(string) string) *GuideSpec {
	g.substitute = fn
	return g
}

// Flow appends a flow to the guide.
func (g *GuideSpec) Flow(f *GuideFlowBuilder) *GuideSpec {
	g.flows = append(g.flows, f)
	return g
}

// ruleAllows reports whether a rule's gate passes for p. A platform predicate
// wins over the feature pair when set, matching step and branch gating.
func ruleAllows(r gatedRule, p hostenv.PlatformProfile) bool {
	if r.pred != nil {
		return r.pred(p)
	}
	return gateAllows(r.when, r.unless, p)
}

// Resolve materializes the AgentGuide for the given platform profile.
func (g *GuideSpec) Resolve(p hostenv.PlatformProfile) AgentGuide {
	sub := g.substitute
	if sub == nil {
		sub = func(s string) string { return s }
	}
	out := AgentGuide{}
	if g.hasSummary {
		out.Summary = sub(g.summary.Resolve(p))
	}
	for _, r := range g.rules {
		if ruleAllows(r, p) {
			out.Rules = append(out.Rules, sub(r.text))
		}
	}
	for _, f := range g.flows {
		out.Flows = append(out.Flows, f.resolve(p, sub))
	}
	return out
}
