package catalog

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// Compiler turns a Catalog into one frontend's native command/tool surface. T
// is the frontend-specific element type: the CLI compiler is a
// Compiler[*cli.Command], the MCP compiler is a Compiler[ToolDescriptor]. A
// generic-typed Compile means callers get back a concrete []T with no `any`
// assertion, while still sharing the one "compile a catalog" abstraction.
type Compiler[T any] interface {
	// Compile maps every operation in cat to the frontend's native shape as
	// []T. The CLI compiler emits []*cli.Command; the MCP compiler emits
	// []ToolDescriptor.
	Compile(cat Catalog) ([]T, error)
}

// ForceFlagName is the boolean confirm flag the CLI compiler adds to every
// SafetyDestructive operation. When it is not set the command's Action refuses
// to run, matching how the existing CLI gates force operations behind --force.
const ForceFlagName = "force"

// NewCLICompiler returns an urfave/cli/v3 compiler (a Compiler[*cli.Command])
// that maps a Catalog to []*cli.Command.
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
func (c *cliCompiler) Compile(cat Catalog) ([]*cli.Command, error) {
	if cat == nil {
		return nil, fmt.Errorf("catalog: cannot compile a nil catalog")
	}
	// VisibilityBoth is treated as unrestricted by the registry, so this
	// returns every registered operation regardless of visibility.
	ops := cat.Search("", "", VisibilityBoth)
	cmds := make([]*cli.Command, 0, len(ops))
	for _, op := range ops {
		cmd, err := commandFor(op)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

// commandFor builds a single *cli.Command from an Operation descriptor. It
// returns an error if a destructive operation declares an arg whose name collides
// with the reserved --force confirm gate, which would otherwise produce a
// duplicate --force flag and a urfave 'flag redefined' error at runtime.
func commandFor(op Operation) (*cli.Command, error) {
	destructive := op.Safety() == SafetyDestructive

	cmd := &cli.Command{
		Name:        op.Name(),
		Usage:       op.Summary(),
		Description: op.Description(),
		ArgsUsage:   op.Positional(),
		Flags:       flagsFor(op),
		Action:      actionFor(op),
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
	if op.Interaction() == InteractionHumanOnly {
		cmd.Usage = op.Summary() + " (requires interactive human input)"
	}

	return cmd, nil
}

// flagsFor maps each OperationArg to a urfave flag of the matching type.
func flagsFor(op Operation) []cli.Flag {
	args := op.Args()
	if len(args) == 0 {
		return nil
	}
	flags := make([]cli.Flag, 0, len(args))
	for _, a := range args {
		flags = append(flags, flagFor(a))
	}
	return flags
}

// flagFor converts a single OperationArg into its urfave flag. The ArgType to
// flag mapping matches the JSON-Schema mapping used by the MCP layer.
func flagFor(a OperationArg) cli.Flag {
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
	// default (normalizeInputDefaults fills it before the Handler runs), so it
	// must not be flagged Required in urfave or the CLI would refuse to run
	// without an explicit value, contradicting the default. Requiredness is the
	// single shared predicate isRequiredArg, used identically by Invoke and the
	// JSON-Schema builder.
	required := isRequiredArg(a)

	switch a.Type {
	case ArgTypeBool, ArgTypeNullableBool:
		return &cli.BoolFlag{Name: a.Name, Usage: help, Value: a.Default == "true", Required: required}
	case ArgTypeInt:
		return &cli.IntFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required}
	case ArgTypeFloat:
		return &cli.Float64Flag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required}
	case ArgTypeDuration:
		return &cli.DurationFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required}
	case ArgTypeStringSlice:
		return &cli.StringSliceFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required}
	default: // ArgTypeString
		return &cli.StringFlag{Name: a.Name, Usage: help, DefaultText: a.Default, Required: required}
	}
}

// actionFor returns the urfave ActionFunc adapter that dispatches to the
// operation's Handler. It builds an input map from the parsed flags, enforces
// the --force confirm gate for destructive operations and the required-arg
// contract, then prints the Handler's result.
func actionFor(op Operation) cli.ActionFunc {
	destructive := op.Safety() == SafetyDestructive

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
				// when Required AND has no default; otherwise normalizeInputDefaults
				// satisfies it.
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
		// Final unified check shared with Invoke: catches a set-but-empty or nil
		// required value that slipped past cmd.IsSet (e.g. a zero-length slice),
		// so the CLI and Invoke reject identical inputs.
		if missing := firstMissingRequiredArg(op.Args(), input); missing != nil {
			return fmt.Errorf("missing required argument --%s", missing.Name)
		}
		// Apply declared defaults uniformly with the Invoke path, so the Handler
		// receives identical input no matter which frontend dispatched.
		normalized, err := normalizeInputDefaults(op.Args(), input)
		if err != nil {
			return err
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

// cliArgValue is the single source of truth for how each ArgType surfaces from
// a parsed urfave command into the operation input map. Both the compiled
// command path (actionFor) and the wiring adapters (FlagValue) delegate to it,
// so a new ArgType needs exactly one mapping instead of a copy per presentation
// adapter.
//
// It returns:
//   - value: the input-map value. When not set, nullable bool yields nil
//     (tri-state "absent"); every other type yields its flag's zero value,
//     which matches what the wiring adapters have always placed in the map.
//   - set:   whether the flag was explicitly provided (cmd.IsSet).
//   - empty: for required-arg validation, whether a provided value is "empty"
//     (only meaningful for string / string-slice types).
func cliArgValue(cmd *cli.Command, a OperationArg) (value any, set bool, empty bool) {
	set = cmd.IsSet(a.Name)
	switch a.Type {
	case ArgTypeBool:
		return cmd.Bool(a.Name), set, false
	case ArgTypeNullableBool:
		// Preserve tri-state: absent flag -> nil, provided -> &bool. c.Bool
		// alone cannot distinguish --flag=false from an absent flag, so gate
		// on set so the Handler sees the same shape as the MCP surface.
		if !set {
			return nil, false, false
		}
		v := cmd.Bool(a.Name)
		return &v, true, false
	case ArgTypeInt:
		// An explicit 0 is a legitimate value (e.g. --ttl 0); presence is
		// determined by set, not by magnitude.
		return cmd.Int(a.Name), set, false
	case ArgTypeFloat:
		return cmd.Float(a.Name), set, false
	case ArgTypeDuration:
		return cmd.Duration(a.Name), set, false
	case ArgTypeStringSlice:
		v := cmd.StringSlice(a.Name)
		return v, set, len(v) == 0
	default: // ArgTypeString
		v := cmd.String(a.Name)
		return v, set, v == ""
	}
}

// FlagValue maps a parsed urfave command to the operation-input value for an
// argument, delegating to cliArgValue. It exists for the CLI wiring adapters
// (catalog_wiring and friends) which place a value for every declared arg into
// the input map and do not gate on flag presence themselves.
func FlagValue(cmd *cli.Command, a OperationArg) any {
	v, _, _ := cliArgValue(cmd, a)
	return v
}
