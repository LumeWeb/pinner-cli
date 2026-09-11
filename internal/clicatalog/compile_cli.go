package clicatalog

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner/catalogmeta"
)

// Package clicatalog hosts the urfave/cli/v3 frontend for the shared pinner
// operation catalog. The catalog core model is go.lumeweb.com/opmesh
// (frontend-neutral operations, registry, normalization/execution) and the
// operation definitions live in the module's go.lumeweb.com/pinner/catalogops;
// CLI presentation metadata (PositionalOnly/AgentOnly/Sources) is keyed by
// stable operation ID in go.lumeweb.com/pinner/catalogmeta. This package
// keeps the CLI-only compiler (and its urfave dependency) separate so the
// shared surface never imports urfave/cli or pterm.

// ForceFlagName is the boolean confirm flag the CLI leaf builder adds to every
// SafetyDestructive operation. The consuming CLI mount's gate factories read
// it to decide whether to require --force before running a destructive op.
const ForceFlagName = "force"

// Compiler turns a Catalog into one frontend's native command/tool surface. T
// is the frontend-specific element type: the CLI compiler is a
// Compiler[*cli.Command], the MCP compiler (in the pinner module) is a
// Compiler[opmesh.ToolDescriptor]. A generic-typed Compile means callers get
// back a concrete []T with no `any` assertion, while still sharing the one
// "compile a catalog" abstraction.
type Compiler[T any] interface {
	// Compile maps every operation in cat to the frontend's native shape as
	// []T. The CLI compiler emits []*cli.Command; the MCP compiler emits
	// []ToolDescriptor.
	Compile(cat opmesh.Catalog) ([]T, error)
}

// NewCLICompiler returns an urfave/cli/v3 compiler (a Compiler[*cli.Command])
// that maps a Catalog to []*cli.Command. This is the legacy whole-catalog
// compile API, retained for the pre-shared-adapter internal/cli wiring; the new
// declarative command-shape compiler is available separately via NewCLILeaf.
func NewCLICompiler() Compiler[*cli.Command] { return &cliCompiler{} }

// cliCompiler maps a Catalog's operations to urfave/cli/v3 *cli.Command values.
// It consumes the underlying Operation directly (it needs the declared Metadata
// AND the Handler to wire into the command's Action), which is why its element
// type differs from the MCP compiler's.
//
// Each operation's Name() (e.g. "vault.create") is used verbatim as the
// *cli.Command Name. urfave tolerates dotted names, and using the full declared
// name keeps the mapping unambiguous across categories: two categories can both
// declare a "create" leaf, so flattening to leaf names would collide.
type cliCompiler struct{}

// Compile converts every operation in cat into a []*cli.Command.
func (c *cliCompiler) Compile(cat opmesh.Catalog) ([]*cli.Command, error) {
	if cat == nil {
		return nil, fmt.Errorf("catalog: cannot compile a nil catalog")
	}
	// VisibilityBoth is treated as unrestricted by the registry, so this
	// returns every registered operation regardless of visibility.
	ops := cat.Search("", "", opmesh.VisibilityBoth)
	cmds := make([]*cli.Command, 0, len(ops))
	for _, op := range ops {
		cmd, err := commandFor(op)
		if err != nil {
			return nil, err
		}
		// The shared commandFor leaves Action nil for the declarative
		// NewCLILeaf path; the legacy whole-catalog compiler wires the action
		// adapter itself so internal/cli keeps its prior behavior.
		cmd.Action = actionFor(op)
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

// NewCLILeaf builds an urfave/cli/v3 *cli.Command shape node for one catalog
// operation using the resolved shape fields (primary display name, category,
// aliases) from the declarative shape model. It wires everything the compiler
// owns — usage, description, args-usage, flags (incl. the destructive --force
// gate) and the HumanOnly usage note — via commandFor, which leaves Action nil
// so the consuming CLI package's leaf builder can attach its catalog action
// adapter (flags and behavior stay mount-owned). This is the
// only place clicatalog constructs a *cli.Command from a resolved LeafLocator;
// it is neutral with respect to internal/cli — cfg is never named here, and
// the caller (internal/cli) sets the Action and relaxFlagRequired.
func NewCLILeaf(op opmesh.Operation, name, category string, aliases []string) (*cli.Command, error) {
	cmd, err := commandFor(op)
	if err != nil {
		return nil, err
	}
	cmd.Name = name
	cmd.Category = category
	cmd.Aliases = aliases
	return cmd, nil
}

// commandFor builds a single *cli.Command from an Operation descriptor. It
// keeps Action nil: behavior is attached by the consuming CLI mount via its
// catalog action adapter (see NewCLILeaf), never here. The legacy whole-catalog
// compiler (cliCompiler.Compile) sets the Action itself via actionFor after the
// fact. commandFor returns an error if a destructive operation declares an arg
// whose name collides with the reserved --force confirm gate, which would
// otherwise produce a duplicate --force flag and a urfave 'flag redefined'
// error at runtime.
func commandFor(op opmesh.Operation) (*cli.Command, error) {
	destructive := op.Safety() == opmesh.SafetyDestructive

	cmd := &cli.Command{
		Name:        op.Name(),
		Usage:       op.Summary(),
		Description: op.Description(),
		ArgsUsage:   op.Positional(),
		Flags:       flagsFor(op),
	}

	// A destructive operation always gets a --force confirm gate. Guard
	// against an operation declaring an arg literally named "force", which
	// would shadow/collide with the synthetic gate flag.
	if destructive {
		for _, a := range op.Args() {
			if a.Name == ForceFlagName {
				return nil, fmt.Errorf("operation %q declares an arg named %q, which is reserved for the destructive confirm gate", op.Name(), ForceFlagName)
			}
		}
		cmd.Flags = append(cmd.Flags, &cli.BoolFlag{
			Name:  ForceFlagName,
			Usage: "Confirm and proceed with this destructive operation",
		})
	}

	// Human-only operations remain runnable by a human at the CLI, so we do
	// not hide them, but flag the intent for future frontends (e.g. MCP, which
	// must refuse them for model agents) with a usage note.
	if op.Interaction() == opmesh.InteractionHumanOnly {
		cmd.Usage = op.Summary() + " (requires interactive human input)"
	}

	return cmd, nil
}

// flagsFor maps each OperationArg to a urfave flag of the matching type. An
// arg marked PositionalOnly (its value is supplied by the command's positional
// argument, e.g. the DNS ops' "zone") or AgentOnly (exposed only on the
// agent/MCP surface, e.g. a type discriminator the CLI derives automatically)
// is skipped: it has no --flag, so no redundant flag appears in help and the
// CLI caller is never asked for it. MCP and direct Invoke are unaffected —
// they read op.Args() directly.
//
// The opmesh core model deliberately carries no frontend fields, so the
// PositionalOnly/AgentOnly carve-outs (and the per-arg env-var Sources) are
// read from the module's frontend-metadata boundary
// (catalogmeta.ArgFrontendForArg), keyed by the stable operation ID. Absent
// entries mean "no frontend specialization", which is the default for every
// operation that declares none.
func flagsFor(op opmesh.Operation) []cli.Flag {
	args := op.Args()
	if len(args) == 0 {
		return nil
	}
	flags := make([]cli.Flag, 0, len(args))
	for _, a := range args {
		if meta := catalogmeta.ArgFrontendForArg(op.Name(), a.Name); meta != nil && (meta.PositionalOnly || meta.AgentOnly) {
			continue
		}
		flags = append(flags, flagFor(op.Name(), a))
	}
	return flags
}

// flagFor converts a single OperationArg into its urfave flag. The ArgType to
// flag mapping matches the JSON-Schema mapping used by the MCP layer.
func flagFor(opID string, a opmesh.OperationArg) cli.Flag {
	help := a.Help
	// urfave/cli/v3 core has no dedicated sensitive flag; mark the usage so
	// shell history / help output discourages passing secrets inline.
	if a.Sensitive {
		if help != "" {
			help += " "
		}
		help += "(sensitive)"
	}
	// An arg that is Required but declares a Default is satisfied by that
	// default (NormalizeOperationInput fills it before the Handler runs), so it
	// must not be flagged Required in urfave or the CLI would refuse to run
	// without an explicit value, contradicting the default. Requiredness is the
	// single shared predicate isRequiredArg, used identically by Invoke and the
	// JSON-Schema builder.
	required := isRequiredArg(a)

	sources := flagSources(opID, a.Name)
	switch a.Type {
	case opmesh.ArgTypeBool, opmesh.ArgTypeNullableBool:
		return &cli.BoolFlag{Name: a.Name, Usage: help, Value: a.Default == "true", Required: required, Sources: sources}
	case opmesh.ArgTypeInt, opmesh.ArgTypeNullableInt:
		return &cli.IntFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	case opmesh.ArgTypeFloat:
		return &cli.Float64Flag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	case opmesh.ArgTypeDuration:
		return &cli.DurationFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	case opmesh.ArgTypeStringSlice:
		return &cli.StringSliceFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	case opmesh.ArgTypeRawJSON:
		// A raw-JSON arg's CLI flag is a plain string carrying JSON text. The
		// handler parses it (e.g. --where-json '[{"tag":"finance"}]').
		return &cli.StringFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	default: // opmesh.ArgTypeString
		return &cli.StringFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required, Sources: sources}
	}
}

// flagSources maps an arg's declared environment Sources onto the urfave flag's
// value-source chain. An empty set yields an empty chain (no env source), so
// this restores the legacy per-flag EnvVars capability for catalog ops without
// changing flags that declare no sources. Sources are frontend metadata
// (catalogmeta.ArgFrontendForArg): the opmesh core model carries none.
func flagSources(opID, argName string) cli.ValueSourceChain {
	if meta := catalogmeta.ArgFrontendForArg(opID, argName); meta != nil {
		return cli.EnvVars(meta.Sources...)
	}
	return cli.EnvVars()
}

// isRequiredArg mirrors the pinner module's unexported same-named predicate:
// an arg is only mandatory when Required AND has no default (a declared default
// satisfies it before the Handler runs). It is a trivial exported-field
// predicate, so no round-trip through an unexported-module shim is needed.
func isRequiredArg(a opmesh.OperationArg) bool {
	return a.Required && a.Default == ""
}

// actionFor returns the urfave ActionFunc adapter that dispatches to the
// operation's Handler. It builds an input map from the parsed flags, enforces
// the --force confirm gate for destructive operations and the required-arg
// contract, then prints the Handler's result. It is used by the legacy
// whole-catalog compiler (cliCompiler) so internal/cli keeps its prior
// command behavior; the declarative NewCLILeaf path leaves Action unset.
func actionFor(op opmesh.Operation) cli.ActionFunc {
	destructive := op.Safety() == opmesh.SafetyDestructive

	return func(ctx context.Context, cmd *cli.Command) error {
		// Destructive confirm gate: refuse unless --force was passed.
		if destructive && !cmd.Bool(ForceFlagName) {
			return fmt.Errorf("operation %q is destructive: pass --%s to confirm", op.Name(), ForceFlagName)
		}

		input := make(map[string]any, len(op.Args()))
		for _, a := range op.Args() {
			value, set, empty := cliArgValue(cmd, a)
			if !set {
				// Requiredness uses the shared isRequiredArg predicate (same one
				// Invoke and the schema builder use): an arg is only mandatory
				// when Required AND has no default; otherwise
				// NormalizeOperationInput satisfies it.
				if isRequiredArg(a) {
					return fmt.Errorf("missing required argument --%s", a.Name)
				}
				continue
			}
			if isRequiredArg(a) && empty {
				return fmt.Errorf("required argument --%s was empty", a.Name)
			}
			input[a.Name] = value
		}
		// Final unified step shared with Invoke: coerces present values into
		// their declared ArgType shape and applies declared defaults uniformly
		// with the Invoke path, so the Handler receives identical input no
		// matter which frontend dispatched. It also re-checks required args
		// (clirRequiredArgError re-surfaces those in the CLI-facing --flag
		// spelling). The unknown-argument and reserved-key stripping inside
		// NormalizeOperationInput are no-ops here: the input map only ever
		// holds declared flag names.
		normalized, err := opmesh.NormalizeOperationInput(op, input)
		if err != nil {
			// Re-surface a missing required arg with the CLI-facing --flag
			// spelling so user-facing help stays flag-oriented.
			return cliRequiredArgError(op, err)
		}
		input = normalized

		h := op.Handler()
		if h == nil {
			return fmt.Errorf("operation %q has no handler", op.Name())
		}
		result, err := h.Execute(ctx, input)
		if err != nil {
			return err
		}
		if result != nil {
			fmt.Printf("%v\n", result)
		}
		return nil
	}
}

// cliRequiredArgError rewords the module's `missing required argument "name"`
// dispatch error into the CLI's `missing required argument --name` spelling so
// the user is pointed at the flag to pass. Any other error passes through
// unchanged.
func cliRequiredArgError(op opmesh.Operation, err error) error {
	const prefix = `missing required argument "`
	msg := err.Error()
	if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		name := msg[len(prefix) : len(msg)-1] // strip trailing closing quote
		for _, a := range op.Args() {
			if a.Name == name {
				return fmt.Errorf("missing required argument --%s", a.Name)
			}
		}
	}
	return err
}

// cliArgValue is the single source of truth for how each ArgType surfaces from
// a parsed urfave command into the operation input map. Every wiring adapter
// (FlagValue / FlagsToInput) delegates to it, so a new ArgType needs exactly
// one mapping instead of a copy per presentation adapter.
//
// It returns:
//   - value: the input-map value. When not set, nullable bool yields nil
//     (tri-state "absent"); every other type yields its flag's zero value,
//     which matches what the wiring adapters have always placed in the map.
//   - set:   whether the flag was explicitly provided (cmd.IsSet).
//   - empty: for required-arg validation, whether a provided value is "empty"
//     (only meaningful for string / string-slice types).
func cliArgValue(cmd *cli.Command, a opmesh.OperationArg) (value any, set bool, empty bool) {
	set = cmd.IsSet(a.Name)
	switch a.Type {
	case opmesh.ArgTypeBool:
		return cmd.Bool(a.Name), set, false
	case opmesh.ArgTypeNullableBool:
		// Preserve tri-state: absent flag -> nil, provided -> &bool. c.Bool
		// alone cannot distinguish --flag=false from an absent flag, so gate
		// on set so the Handler sees the same shape as the MCP surface.
		if !set {
			return nil, false, false
		}
		v := cmd.Bool(a.Name)
		return &v, true, false
	case opmesh.ArgTypeInt:
		// An explicit 0 is a legitimate value (e.g. --ttl 0); presence is
		// determined by set, not by magnitude.
		return cmd.Int(a.Name), set, false
	case opmesh.ArgTypeNullableInt:
		// Preserve tri-state: absent flag -> nil, provided -> *int. The int
		// flag alone cannot distinguish --priority 0 from an absent flag, so
		// gate on set so the Handler sees the same shape as the MCP surface.
		if !set {
			return nil, false, false
		}
		v := cmd.Int(a.Name)
		return &v, true, false
	case opmesh.ArgTypeFloat:
		return cmd.Float(a.Name), set, false
	case opmesh.ArgTypeDuration:
		return cmd.Duration(a.Name), set, false
	case opmesh.ArgTypeStringSlice:
		v := cmd.StringSlice(a.Name)
		return v, set, len(v) == 0
	case opmesh.ArgTypeRawJSON:
		// The CLI surface of a raw-JSON arg is a string (JSON text).
		v := cmd.String(a.Name)
		return v, set, v == ""
	default: // opmesh.ArgTypeString
		v := cmd.String(a.Name)
		return v, set, v == ""
	}
}

// FlagValue maps a parsed urfave command to the operation-input value for an
// argument, delegating to cliArgValue. It exists for the CLI wiring adapters
// (catalog_wiring and friends) which place a value for every declared arg into
// the input map and do not gate on flag presence themselves.
func FlagValue(cmd *cli.Command, a opmesh.OperationArg) any {
	v, _, _ := cliArgValue(cmd, a)
	return v
}

// FlagsToInput builds the operation input map for every declared argument from
// a parsed urfave command, applying the same flag->value mapping as FlagValue.
// It is the single canonical construction used by the CLI action adapters so
// they do not each hand-iterate over op.Args(); MCP never calls it (the MCP
// layer reads only the flag *schema*, never flag values).
func FlagsToInput(cmd *cli.Command, op opmesh.Operation) map[string]any {
	args := op.Args()
	if len(args) == 0 {
		return nil
	}
	input := make(map[string]any, len(args))
	for _, a := range args {
		input[a.Name] = FlagValue(cmd, a)
	}
	return input
}
