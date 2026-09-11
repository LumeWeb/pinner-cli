package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/pinner/core/pinning"
)

// catalog_wiring.go adapts the pins operations in
// go.lumeweb.com/pinner/catalogops to the urfave/cli/v3 command tree, mounting
// them under the "pins" parent command and rendering each handler's data result
// through the CLI Output formatter.
//
// internal/catalogops exposes PinsDeps and PinsOperations and never renders or
// imports pkg/cli. This file maps CLI concerns onto the catalog: positional CID
// args, --file/stdin CID reads, the --force/--confirm gate, requireUpdateFields,
// dry-run passthrough, and result rendering.
//
// Command shape (nesting, canonical names, aliases) is declared in
// internal/clicatalog/shapes_pins.go and materialized by CompileCommandTree
// through the mount-owned buildPinsLeaf and buildCLIParent builders — naming is
// a compiler concern, not a hand-written mapping here. What stays mount-owned
// here is CLI-only I/O and behavior: positional <cid>/<cid...> resolution,
// --file/stdin reads, the destructive gate, and the result renderer, wired via
// pinsCatalogConfig and the shared catalog action adapter.

// catalogPinningDeps builds the catalogops.PinsDeps from the live CLI wiring.
// Service construction uses a discard writer so handlers return pure data and
// NEVER render; all presentation happens in renderCatalogResult.
func catalogPinningDeps(factory ...ConfigManagerFactory) catalogops.PinsDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.PinsDeps{
		// Lazy config manager: resolved per invocation, never at package init.
		CfgMgr: func() config.Manager {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Secure: func() bool {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return false
			}
			return GetSecureSetting(nil, cfgMgr)
		},
		ServiceFactory: defaultPinningServiceFactory,
		NewAuthenticated: func(cfgMgr config.Manager, secure bool, token string) pinning.PinningService {
			discard := NewOutputFormatter(false, false, false, false)
			discard.SetWriter(io.Discard)
			return NewPinningService(cfgMgr, discard, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(token))
		},
		GetAuthToken: func() string {
			// Read the auth token live from config. Handlers that use
			// NewAuthenticated get a service pinned to this token; when unset
			// they fall back to ServiceFactory, which also reads config.
			cfgMgr, err := cfgFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

// pinsCatalogDeps holds the catalogops.PinsDeps so the wiring and the renderer
// can both reach the canonical operation list without rebuilding it.
var pinsCatalogDeps = catalogops.PinsDeps(catalogPinningDeps())

// newPinsCommand is the catalog-driven "pins" parent command. Its command shape
// is declared in internal/clicatalog/shapes_pins.go and materialized through
// mount-owned leaf and parent builders. Flag injection (--file/--no-wait/
// --yes), positional CID resolution, destructive gating, and the renderer all
// stay mount-owned here via pinsCatalogConfig.
func newPinsCommand() *cli.Command {
	root := clicatalog.PinsDomainRoot
	ops := catalogops.PinsOperations(pinsCatalogDeps)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.PinsShapes,
		root,
		pinsCatalogConfig(),
		buildPinsLeaf,
		buildCLIParent,
	)
	if err != nil {
		panic(fmt.Sprintf("catalog compile pins: %v", err))
	}

	return &cli.Command{
		Name:        root.Name,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands:    cmds,
	}
}

// buildPinsLeaf is the mount-owned leaf builder materializing one pins leaf
// into an urfave *cli.Command. Shape (name/category/aliases/flags/usage) comes
// from the clicatalog model via NewCLILeaf; behavior (positional/file/stdin
// resolution, destructive gate, field-required gate) stays mount-owned here via
// the catalog action adapter + relaxFlagRequired.
//
// Flag injection is the one pins-specific addition beyond the generic leaf
// builder: --file/--no-wait on "add" and --file/--yes on "rm". These are CLI
// convenience flags that the catalog ops' Args() do not declare but the
// adapter's ResolvePositional/MutateInput/ExtraConfirm read, so they must be
// appended here (preserving the mountCatalogCommand behavior).
func buildPinsLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	switch loc.Op.Name() {
	case "pins_add":
		base.Flags = append(base.Flags, FileFlag(), NoWaitFlag())
	case "pins_rm":
		base.Flags = append(base.Flags, FileFlag(), YesFlag())
	}
	// Clear urfave-required markers so positionally-supplied CIDs (status/
	// update) can reach the handler; the adapter re-enforces requiredness on
	// empty values. Without this, urfave rejects `pins status <cid>` at parse
	// time before the positional→input mapping ever runs.
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// pinsCatalogConfig returns the CatalogAdapterConfig that expresses pins'
// exact per-invocation behavior on top of the shared catalogActionAdapter
// pipeline. Pins needs seven hooks: the result renderer, the per-invocation
// --auth-token override, the cids/cid positional multi-source resolution
// (stdin-pipe > --file > positional for add/rm; first positional→cid for
// status/update), the --no-wait→wait surgery, the destructive --force gate for
// rm (hint to stdout + exit 0 when unconfirmed with a target, fall-through
// when no target, dry-run bypass), the rm --all typed-count prompt, and the
// update at-least-one-field guard. All remaining fields stay nil so the shared
// pipeline's safe defaults apply.
//
// The old adapter took a `group` ("pins_") parameter, but every pins operation
// is canonically "pins_<leaf>" and the pins mount always passes "pins_", so
// the group is reachable per-invocation via ic.Op.Name() (the leaf being the
// last underscore segment) and ic.C (the mounted command). This config needs
// no arguments.
func pinsCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderCatalogResult,
		HonorAuthTokenOverride: true,

		// Resolve positional/file/stdin CID sources for the commands that
		// accept them (add/rm take cids via stdin-pipe > --file > positional
		// priority; status/update take a single positional cid). Must run
		// before the destructive gate so rm's hint logic can inspect the
		// resolved cids.
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			switch ic.Op.Name() {
			case "pins_add", "pins_rm":
				resolved, err := resolveCidsInput(ic.C)
				if err != nil {
					return err
				}
				ic.Input["cids"] = resolved
			case "pins_status", "pins_update":
				return posFirstToNamed(ic, "cid")
			}
			return nil
		},

		// --no-wait maps onto the catalog's --wait (wait defaults true).
		MutateInput: func(ic *CatalogInvokeContext) error {
			if ic.C.IsSet(FlagNoWait) && ic.C.Bool(FlagNoWait) {
				ic.Input["wait"] = false
			}
			return nil
		},

		// Destructive gate (pins rm). The shared GateHintSilent reproduces the
		// old adapter exactly: write confirm (+ all when --all) and, for an
		// unconfirmed non-dry-run with a target (--all or cids present), print
		// a hint to stdout and exit 0; with no target, fall through so the
		// handler's "no CIDs provided" validation produces a non-zero exit.
		DestructiveGate: GateHintSilent(
			func(ic *CatalogInvokeContext) bool {
				return ic.C.Bool(FlagAll) || len(opmesh.StrSliceArg(ic.Input, "cids")) > 0
			},
			func(ic *CatalogInvokeContext, _ Output) string {
				if ic.C.Bool(FlagAll) {
					return "Use --force to unpin all pins. This is a destructive operation."
				}
				return "Use --force to unpin CID: " + opmesh.StrSliceArg(ic.Input, "cids")[0]
			},
		),

		// Require the operator to type the pinned count (or pass --yes/--force)
		// before an unpin-all. The hidden --confirm alias only satisfies the
		// outer destructive gate (it lets the operation proceed); it does not
		// bypass the typed-count prompt, which still requires --force or --yes.
		// This is CLI-only: catalogops stays IO-agnostic, and the MCP/programmatic
		// path is non-interactive (passes --force).
		ExtraConfirm: func(ic *CatalogInvokeContext) error {
			if ic.Op.Name() != "pins_rm" || !ic.C.Bool(FlagAll) || ic.DryRun || ic.C.Bool(FlagYes) || ic.C.Bool(FlagForce) {
				return nil
			}
			svc, svcErr := catalogPinningDeps().Service(ic.Input)
			if svcErr != nil {
				return svcErr
			}
			statusFilter, _ := ic.Input["status"].(string)
			pins, err := svc.List(ic.Ctx, pinning.ListOptions{Status: statusFilter})
			if err != nil {
				return err
			}
			if len(pins) > 0 {
				expected := strconv.Itoa(len(pins))
				prompter := &PTermConfirmPrompter{}
				result, err := prompter.Confirm(
					fmt.Sprintf("Type %s to confirm unpinning all %d pins", expected, len(pins)),
					expected,
				)
				if err != nil {
					return ErrUnpinAllAborted
				}
				if result != expected {
					return ErrUnpinAllAborted
				}
			}
			return nil
		},

		// Require at least one of --name/--meta/--clear-meta for update.
		UpdateGuard: func(ic *CatalogInvokeContext) error {
			if ic.Op.Name() == "pins_update" {
				if !ic.C.IsSet(FlagName) && !ic.C.IsSet(FlagMeta) && !ic.C.IsSet(FlagClearMeta) {
					return fmt.Errorf("at least one field must be provided for update (--name, --meta, --clear-meta)")
				}
			}
			return nil
		},
	}
}

// renderCatalogResult renders a handler's typed DATA result through the CLI
// Output formatter. It is the single rendering home for catalog-driven commands
// and never touches core services. It is a plain function invoked by the shared
// catalogActionAdapter pipeline (pinsCatalogConfig.Renderer).
func renderCatalogResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *catalogops.DryRunResult:
		renderCatalogDryRun(output, c, r)
		return nil

	case catalogops.ListResult:
		return renderListResult(output, r)

	case *pinning.PinResult:
		output.PrintFields(FieldGroup{Fields: []Field{
			{"CID", r.CID},
			{"Request ID", r.RequestID},
			{"Status", r.Status},
		}})
		return nil

	case *pinning.PinStatus:
		renderPinStatus(output, r)
		return nil

	case *pinning.BatchResult:
		output.PrintBatchResult(r)
		return nil

	case *pinning.UnpinResult:
		if r != nil && r.CID != "" {
			output.Printfln("Unpinned CID: %s", r.CID)
		}
		return nil

	default:
		// No data (nil) or an unexpected type: a command that produced no
		// meaningful result still succeeded.
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// renderCatalogDryRun renders a DryRunResult as the CLI dry-run preview.
func renderCatalogDryRun(output Output, _ *cli.Command, r *catalogops.DryRunResult) {
	output.Printfln("Dry run: %s (no changes made)", r.Operation)
	if len(r.CIDs) > 0 {
		rows := make([][]string, len(r.CIDs))
		for i, cid := range r.CIDs {
			rows[i] = []string{cid, "will " + r.Operation}
		}
		output.PrintTable([]string{"Item", "Action"}, rows)
	}
	// Show the resolved options (name, parallel, etc.) as fields.
	fields := make([]Field, 0, len(r.Options))
	for k, v := range r.Options {
		fields = append(fields, Field{k, v})
	}
	if len(fields) > 0 {
		output.PrintFields(FieldGroup{Title: "Options", Fields: fields})
	}
}

// ---- CLI-input helpers (pkg/cli layer only; not in internal/catalogops) ----

// resolveCidsInput collects CIDs for the add/rm operations from, in priority
// order: stdin pipe, --file, then positional args. Returns an error when
// nothing is supplied.
func resolveCidsInput(c *cli.Command) ([]string, error) {
	var cids []string
	var err error

	switch {
	case isStdinPipe():
		cids, err = readLinesFromStdin()
		if err != nil {
			return nil, fmt.Errorf("failed to read CIDs from stdin: %w", err)
		}
	case c.String(FlagFile) != "":
		cids, err = readCIDsFromFile(c.String(FlagFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read CIDs from file: %w", err)
		}
	default:
		cids = c.Args().Slice()
	}

	var clean []string
	for _, cid := range cids {
		for _, f := range strings.Fields(cid) {
			clean = append(clean, f)
		}
	}
	return clean, nil
}

// relaxFlagRequired clears the urfave-level Required marker on a command's
// single-valued and Bool flags.
//
// The catalog compiler marks required OperationArgs as urfave-required, and
// urfave/cli/v3 fails at parse time when such a flag is not set, before any
// Action runs. But required args that the CLI passes positionally (for example
// `pins rm <cid>`) come through the wiring's positional-to-input mapping, not
// as a --<name> flag. If the marker stayed set, the command would be rejected
// before that mapping runs.
//
// Bool flags are relaxed too: an operation's destructive-confirm arg is backed
// by a separate --force gate, not the compiled --confirm flag (the wiring maps
// --force into input["confirm"]). If the compiled bool stayed required, urfave
// would reject <op> --force at parse time. The gate and the handler re-enforce
// the confirmation before any mutation, so relaxing the marker loses no safety.
//
// StringSlice flags are untouched: their CLI values are multi-valued positional
// and handled separately by the resolver.
func relaxFlagRequired(cmd *cli.Command) {
	for _, f := range cmd.Flags {
		switch flag := f.(type) {
		case *cli.StringFlag:
			flag.Required = false
		case *cli.IntFlag:
			flag.Required = false
		case *cli.Float64Flag:
			flag.Required = false
		case *cli.DurationFlag:
			flag.Required = false
		case *cli.BoolFlag:
			flag.Required = false
		}
	}
}
