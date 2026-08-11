// Package catalog provides the agnostic operation-descriptor registry that
// every frontend (CLI, MCP, and future desktop/server/mobile) derives from.
//
// A catalog operation is declared, not inferred: it carries explicit machine
// properties (Safety, Interaction, Visibility) plus a first-class Handler, so
// frontends never have to reverse-engineer intent from a command's name. The
// catalog is an in-memory Go registry of Operation descriptors; it is not a
// wire format or serialization surface.
package catalog

import "context"

// Safety classifies what an operation does to state. Declared, NOT inferred
// from command name — renaming a command can never silently drop a guard.
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

// ArgType mirrors the urfave/v3 flag type mapping already in the MCP schema layer.
type ArgType int

const (
	// ArgTypeString is a string argument.
	ArgTypeString ArgType = iota
	// ArgTypeBool is a boolean argument.
	ArgTypeBool
	// ArgTypeInt is an integer argument.
	ArgTypeInt
	// ArgTypeFloat is a floating-point argument.
	ArgTypeFloat
	// ArgTypeDuration is a duration argument.
	ArgTypeDuration
	// ArgTypeStringSlice is a slice of strings argument.
	ArgTypeStringSlice
)

// OperationArg describes one input. Drives the JSON Schema (MCP) AND the
// urfave flag (CLI) — one declaration, both views.
type OperationArg struct {
	Name      string
	Type      ArgType
	Required  bool
	Default   string
	Enum      []string
	Sensitive bool
	Help      string // human help (CLI)
	AgentHelp string // agent-oriented help (MCP) — audience separation
}

// Handler.Execute runs the business operation against core. Never touches
// urfave or MCP. Implemented by core services; injected at registry-build time.
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
	Description() string
	AgentDescription() string // MCP describe_tool override (audience split)
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
	Name             string
	Title            string
	Summary          string
	Description      string
	AgentDescription string
	Args             []OperationArg
	Positional       string
	Safety           Safety
	Interaction      Interaction
	Visibility       Visibility
	Category         string
	Handler          Handler
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
func (o simpleOperation) AgentDescription() string { return o.spec.AgentDescription }
func (o simpleOperation) Args() []OperationArg     { return o.spec.Args }
func (o simpleOperation) Positional() string       { return o.spec.Positional }
func (o simpleOperation) Safety() Safety           { return o.spec.Safety }
func (o simpleOperation) Interaction() Interaction { return o.spec.Interaction }
func (o simpleOperation) Visibility() Visibility   { return o.spec.Visibility }
func (o simpleOperation) Category() string         { return o.spec.Category }
func (o simpleOperation) Handler() Handler         { return o.spec.Handler }
