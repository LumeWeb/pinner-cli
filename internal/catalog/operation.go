// Package catalog provides the operation-descriptor registry that every
// frontend (CLI, MCP, desktop/server/mobile) derives from. It is an in-memory
// Go registry of Operation descriptors, not a wire format.
package catalog

import "context"

// Safety classifies what an operation does to state. It is declared on the
// operation, not inferred from a command name.
type Safety int

const (
	// SafetyRead is a pure read with no mutation.
	SafetyRead Safety = iota
	// SafetyMutate mutates state but is non-destructive.
	SafetyMutate
	// SafetyDestructive is irreversible (delete, forget, force ops).
	SafetyDestructive
)

// Interaction declares how a frontend may invoke the operation.
type Interaction int

const (
	// InteractionAgentSafe is fully automatable, no human input/OOB.
	InteractionAgentSafe Interaction = iota
	// InteractionHumanOnly prompts interactively; no agent-safe form.
	InteractionHumanOnly
	// InteractionNeedsHandoff requires external browser/device, split into 2 calls.
	InteractionNeedsHandoff
)

// Visibility declares which consumers may discover/invoke the operation.
type Visibility int

const (
	// VisibilityModel is agent-visible (searchable), the default.
	VisibilityModel Visibility = iota
	// VisibilityAppOnly is an app-only helper; invisible to agent discovery.
	VisibilityAppOnly
	// VisibilityBoth is both agent- and app-visible.
	VisibilityBoth
)

// ArgType matches the urfave/v3 flag type mapping used by the MCP schema layer.
type ArgType int

const (
	// ArgTypeString is a string argument.
	ArgTypeString ArgType = iota
	// ArgTypeBool is a boolean argument.
	ArgTypeBool
	// ArgTypeNullableBool is a tri-state boolean argument: its value may be
	// absent (nil), true, or false, and the Handler receives a *bool so it can
	// distinguish "omitted" from "explicitly false". Unlike ArgTypeBool — whose
	// absence is collapsed to false by normalizeInputDefaults — a nullable bool
	// preserves the absent state, which operations need when omission means
	// "leave unchanged" or "use the backend default" rather than "false".
	ArgTypeNullableBool
	// ArgTypeNullableInt is a tri-state integer argument: its value may be
	// absent (nil) or an explicit *int, and the Handler receives a *int so it
	// can distinguish "omitted" from "explicitly 0". Unlike ArgTypeInt — whose
	// absence is collapsed to 0 by normalizeInputDefaults — a nullable int
	// preserves the absent state, which operations need when omission means
	// "use the backend/op default" rather than "set 0" (e.g. an MX priority
	// that defaults to 10 when omitted).
	ArgTypeNullableInt
	// ArgTypeInt is an integer argument.
	ArgTypeInt
	// ArgTypeFloat is a floating-point argument.
	ArgTypeFloat
	// ArgTypeDuration is a duration argument.
	ArgTypeDuration
	// ArgTypeStringSlice is a slice of strings argument.
	ArgTypeStringSlice
	// ArgTypeFlexibleID is a string argument that also accepts a JSON integer
	// (e.g. a numeric id emitted by ipns_keys_list, whose backend ids are ints).
	// It coerce-renders any accepted numeric to its decimal string form so a
	// Handler reading via StrArg/StrFlexibleArg always receives a string, and
	// it advertises ["string","integer"] in the MCP JSON Schema so a model can
	// pass either the id's integer form or a string form without being
	// rejected by the normalizer.
	ArgTypeFlexibleID
)

// Target is a per-profile presentation variant for the MCP surface only.
// It is catalog-native: Require holds opaque feature-name strings the MCP
// bridge maps to hostenv.FeatureSet. The CLI compiler never reads Targets.
type Target struct {
	// Require lists feature names that must all be present for this target
	// to be eligible. Empty = matches any MCP profile (universal fallback).
	Require []string
	// Visible controls whether the tool appears at all for matching
	// profiles. false = suppress the tool entirely for this profile.
	Visible bool
	// Description is the MCP description for this target. The MCP compiler
	// uses the fallback target's Description as the static descriptor
	// description; per-request resolution may override it with a more
	// specific target's Description.
	Description string
	// DescFunc is a per-profile description resolver. When non-nil, the MCP
	// bridge calls it with the resolved hostenv.PlatformProfile to produce a
	// dynamic, feature-gated description. It overrides Description at
	// resolution time. The parameter type is any because catalog cannot
	// import hostenv; the MCP bridge (catalogsurface.go) performs the cast.
	DescFunc func(any) string
}

// MCPTargets wraps a variadic list of Targets into a slice. Use it in
// OperationSpec declarations for readability.
func MCPTargets(targets ...Target) []Target { return targets }

// TargetFor creates a visible Target that requires all given feature names.
// Among all matching targets, the one with the most required features wins.
func TargetFor(desc string, features ...string) Target {
	return Target{Require: features, Visible: true, Description: desc}
}

// Fallback creates a visible Target with no feature requirements. It always
// matches (score 0), so it only wins when no specific target does. Every
// operation's MCPTargets should include a Fallback to guarantee resolution.
func Fallback(desc string) Target {
	return Target{Visible: true, Description: desc}
}

// Hidden creates an invisible Target that suppresses the tool entirely for
// MCP profiles matching the given features.
func Hidden(features ...string) Target {
	return Target{Require: features, Visible: false}
}

// FallbackFunc creates a visible Target with no feature requirements whose
// description is resolved dynamically via fn at per-request resolution time.
// The MCP bridge calls fn with the resolved hostenv.PlatformProfile, so fn
// is expected to accept any (the bridge casts it). Use it when a tool's MCP
// description is composed from feature-gated segments via toolforge.DescBuilder.
func FallbackFunc(fn func(any) string) Target {
	return Target{Visible: true, DescFunc: fn}
}

// OperationArg describes one input. It drives both the JSON Schema (MCP) and
// the urfave flag (CLI).
type OperationArg struct {
	Name          string
	Type          ArgType
	Required      bool
	Default       string
	Enum          []string
	Sensitive     bool
	Help          string // human help (CLI)
	AgentHelp     string // agent-oriented help (MCP); audience separation
	AgentRequired bool   // required on the MCP surface only; never the CLI
	// AgentOnly marks an arg exposed on the agent/MCP surface only: the CLI
	// compiler omits its --flag entirely (like PositionalOnly), so the value is
	// never requested from a CLI caller. The arg still appears by name in the
	// MCP JSON-Schema and the handler's input map; a CLI invocation simply
	// never supplies it, so the handler must tolerate its absence (a default,
	// or derivation from other inputs). Used for agent-oriented controls that
	// have no meaningful CLI flag (e.g. a type discriminator the CLI derives
	// automatically).
	AgentOnly bool
	// SelectionGroup groups mutually-exclusive selector members: exactly one
	// arg in a group may be selected per invocation (e.g. pins_rm's "cids" and
	// "all"). Empty selects membership in no group. Enforcement is centralized
	// in the normalize path so CLI, MCP, and direct Invoke all agree.
	SelectionGroup string
	// Sources names env vars that also source this arg's value on the CLI
	// surface (restores legacy flag EnvVars support, e.g. PINNER_DOMAIN_NAMESPACE).
	// Empty means the arg is a plain flag with no env source. MCP and direct
	// Invoke ignore Sources.
	Sources []string
	// PositionalOnly marks an arg whose value is supplied by the command's
	// positional argument rather than a --flag (e.g. the DNS ops' "zone", which
	// is also declared as Positional: "<domain>"). The CLI compiler skips
	// emitting a urfave flag for it, eliminating the redundant `--zone string`
	// entry next to `<domain>` in help. MCP and direct Invoke are unaffected:
	// the arg still appears by name in the JSON-Schema and the handler's input
	// map (the CLI wiring adapter fills it from the positional). No-op on args
	// that have no Positional — they must never set it.
	PositionalOnly bool
}

// Handler.Execute runs the business operation against core. It never touches
// urfave or MCP. Implemented by core services.
type Handler interface {
	Execute(ctx context.Context, input map[string]any) (any, error)
}

// Operation is the canonical descriptor consumed by every frontend. It is an
// interface so concrete operations can be different types; the registry holds
// them polymorphically.
type Operation interface {
	Name() string
	Title() string
	Summary() string
	Description() string  // CLI description; the CLI compiler reads this
	MCPTargets() []Target // MCP per-profile targets; MCP-only
	Args() []OperationArg
	Positional() string // ArgsUsage, drives MCP _args
	Safety() Safety
	Interaction() Interaction
	Visibility() Visibility
	Category() string
	Handler() Handler
}

// OperationSpec is the plain struct NewOperation turns into an Operation.
type OperationSpec struct {
	Name        string
	Title       string
	Summary     string
	Description string   // CLI description; the CLI compiler reads this
	MCPTargets  []Target // MCP per-profile targets; MCP-only
	Args        []OperationArg
	Positional  string
	Safety      Safety
	Interaction Interaction
	Visibility  Visibility
	Category    string
	Handler     Handler
}

// NewOperation builds a concrete Operation from a spec. Most operations use
// this; a bespoke operation may implement Operation directly (e.g. one whose
// Handler needs closures over core deps).
func NewOperation(spec OperationSpec) Operation { return simpleOperation{spec} }

// simpleOperation is the default Operation implementation, delegating every
// method to the fields of the spec it was constructed from.
type simpleOperation struct{ spec OperationSpec }

func (o simpleOperation) Name() string             { return o.spec.Name }
func (o simpleOperation) Title() string            { return o.spec.Title }
func (o simpleOperation) Summary() string          { return o.spec.Summary }
func (o simpleOperation) Description() string      { return o.spec.Description }
func (o simpleOperation) MCPTargets() []Target     { return o.spec.MCPTargets }
func (o simpleOperation) Args() []OperationArg     { return o.spec.Args }
func (o simpleOperation) Positional() string       { return o.spec.Positional }
func (o simpleOperation) Safety() Safety           { return o.spec.Safety }
func (o simpleOperation) Interaction() Interaction { return o.spec.Interaction }
func (o simpleOperation) Visibility() Visibility   { return o.spec.Visibility }
func (o simpleOperation) Category() string         { return o.spec.Category }
func (o simpleOperation) Handler() Handler         { return o.spec.Handler }
