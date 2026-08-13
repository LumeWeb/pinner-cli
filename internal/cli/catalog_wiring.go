package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
)

// catalog_wiring.go adapts the operation catalog (internal/catalog) and its
// catalogops pins operations to the urfave/cli/v3 command tree, mounting them
// under the "pins" parent command and rendering each handler's DATA result
// through the CLI Output formatter.
//
// internal/catalogops exposes PinsDeps and PinsOperations and never renders or
// imports pkg/cli. This file maps CLI concerns onto the catalog: positional CID
// args, --file/stdin CID reads, the --force/--confirm gate, requireUpdateFields,
// dry-run passthrough, and result rendering.
//
// The catalog compiler names operations with a dot ("pins.add"). The CLI nests
// them, so the "<group>." prefix is stripped and each leaf is mounted under the
// "pins" parent. That mapping lives here, not in internal/catalog.
//
// The catalog compiler builds the input map from flags only and does not read
// positional args. Commands that take <cid>/<cid...> positionally (pins
// add/rm/status/update) translate the positional args plus --file/stdin into
// the operation's "cids"/"cid" input before dispatch. Each compiled command's
// Action is replaced with an adapter that resolves CLI inputs into the
// operation input map, applies the CLI-only gates, calls op.Handler().Execute,
// and renders the returned data. The flags, help, and names still come from the
// catalog compiler; only the Action is wrapped.

// catalogPinningDeps builds the catalogops.PinsDeps from the live CLI wiring.
// Service construction uses a discard writer so handlers return pure data and
// NEVER render; all presentation happens in renderCatalogResult.
func catalogPinningDeps() catalogops.PinsDeps {
	return catalogops.PinsDeps{
		// Lazy config manager: resolved per invocation, never at package init.
		CfgMgr: func() config.Manager {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Secure: func() bool {
			cfgMgr, err := defaultConfigManagerFactory()
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
			cfgMgr, err := defaultConfigManagerFactory()
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

// newPinsCommand is the catalog-driven "pins" parent command. It compiles the
// pins operations via the catalog's CLI compiler and nests the resulting leaf
// commands under a "pins" group.
func newPinsCommand() *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.PinsOperations(pinsCatalogDeps) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the pins group.
		panic(fmt.Sprintf("catalog compile pins: %v", err))
	}

	cmds := make([]*cli.Command, 0, len(compiled))
	for _, c := range compiled {
		cmds = append(cmds, mountCatalogCommand(c))
	}

	return &cli.Command{
		Name:        "pins",
		Category:    "Pinning",
		Usage:       "Manage pinned content",
		Description: "Manage your pinned IPFS content with subcommands for adding, removing, listing, checking status, and updating pin metadata. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
		Commands:    cmds,
	}
}

// mountCatalogCommand adapts a catalog-compiled command (dotted name like
// "pins.add") into a live CLI subcommand: it strips the "pins." group prefix
// and wraps the Action with the CLI-input adapter (positional/file/stdin to
// operation input, destructive gate, field-required gate). The compiler's flags
// and help text are preserved.
func mountCatalogCommand(cmd *cli.Command) *cli.Command {
	group := "pins_"
	canonicalLeaf := cmd.Name
	if strings.HasPrefix(cmd.Name, group) {
		canonicalLeaf = strings.TrimPrefix(cmd.Name, group)
		// The catalog name is "pins.list"; the CLI exposes the listing
		// subcommand as "ls" (the documented `pinner pins ls`). MCP keeps the
		// canonical "list". The alias lives in the CLI adapter layer only.
		display := canonicalLeaf
		if display == "list" {
			display = "ls"
		}
		cmd.Name = display
	}
	cmd.Category = "Pinning"

	// The adapter reads the --file/--no-wait flags, but catalog-compiled
	// add/rm commands only derive flags from op.Args(), which declare neither.
	// Re-declare them here so `pins add --file cids.txt`, `pins add --no-wait`,
	// and `pins rm --file cids.txt --force` work instead of silently ignoring
	// the file or never reaching no-wait mode.
	switch canonicalLeaf {
	case "add":
		cmd.Flags = append(cmd.Flags, FileFlag(), NoWaitFlag())
	case "rm":
		cmd.Flags = append(cmd.Flags, FileFlag(), YesFlag())
	}

	// Find the canonical operation for this command so the adapter and
	// renderer can dispatch to its Handler. Lookup uses the catalog's
	// canonical (dotted) name, independent of the CLI display alias.
	var op catalog.Operation
	for _, cand := range catalogops.PinsOperations(pinsCatalogDeps) {
		if cand.Name() == group+canonicalLeaf {
			op = cand
			break
		}
	}
	if op != nil {
		cmd.Action = catalogActionAdapter(op, group)
	}
	// Clear urfave-required markers so positionally-supplied CIDs (status/
	// update) can reach the handler; the adapter re-enforces requiredness on
	// empty values. Without this, urfave rejects `pins status <cid>` at parse
	// time before the positional→input mapping ever runs.
	relaxFlagRequired(cmd)
	return cmd
}

// catalogActionAdapter returns the per-invocation ActionFunc for a catalog
// operation. It resolves CLI inputs into the operation's input map, applies
// the CLI-only gates described in the package comment, invokes the handler,
// and renders the result through the Output formatter.
func catalogActionAdapter(op catalog.Operation, group string) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		output := setupOutput(c)
		cfgMgr, err := defaultConfigManagerFactory()
		if err != nil {
			return err
		}
		_ = cfgMgr // deps are wired globally; cfgMgr kept for parity

		// Build the input map from the compiler-declared flags plus the
		// resolved CLI inputs (positional/file/stdin → "cids"/"cid").
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// The per-invocation --auth-token flag takes precedence over the config
		// token. Only set it when provided so deps.service() falls back to the
		// config-read GetAuthToken otherwise.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Resolve positional/file/stdin CID sources for the commands that
		// accept them (add/rm take cids; status/update take a single cid).
		switch op.Name() {
		case group + "add", group + "rm":
			resolved, err := resolveCidsInput(c)
			if err != nil {
				return err
			}
			input["cids"] = resolved
		case group + "status", group + "update":
			if cid := positionalCID(c); cid != "" && catalog.StrArg(input, "cid", "") == "" {
				input["cid"] = cid
			}
		}

		// --no-wait maps onto the catalog's --wait (wait defaults true).
		if c.IsSet(FlagNoWait) && c.Bool(FlagNoWait) {
			input["wait"] = false
		}

		// Destructive gate (pins rm). The catalog compiler injects a --force
		// confirm gate into the compiled Action, but since we replace the Action
		// here we enforce it ourselves, honoring both --force and the hidden
		// --confirm alias.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			input["confirm"] = confirm
			if c.Bool(FlagAll) {
				input["all"] = true
			}
			// Unconfirmed destructive operations print a guard hint and refuse.
			if !confirm && !c.Bool(FlagDryRun) {
				switch {
				case c.Bool(FlagAll):
					output.Printfln("Use --force to unpin all pins. This is a destructive operation.")
					return nil
				case len(catalog.StrSliceArg(input, "cids")) > 0:
					output.Printfln("Use --force to unpin CID: %s", catalog.StrSliceArg(input, "cids")[0])
					return nil
				default:
					// Nothing to delete: fall through to the handler so its
					// "no CIDs provided" validation produces a non-zero exit,
					// instead of silently succeeding.
				}
			}
		}

		// Require the operator to type the pinned count (or pass --yes/--force)
		// before an unpin-all. The hidden --confirm alias only satisfies the
		// outer destructive gate (it lets the operation proceed); it does not
		// bypass the typed-count prompt, which still requires --force or --yes.
		// This is CLI-only: catalogops stays IO-agnostic, and the MCP/programmatic
		// path is non-interactive (passes --force).
		if op.Name() == "pins_rm" && c.Bool(FlagAll) && !c.Bool(FlagDryRun) && !c.Bool(FlagYes) && !c.Bool(FlagForce) {
			svc, svcErr := catalogPinningDeps().Service(input)
			if svcErr != nil {
				return svcErr
			}
			statusFilter, _ := input["status"].(string)
			pins, err := svc.List(ctx, "", 0, statusFilter)
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
		}

		// Require at least one of --name/--meta/--clear-meta for update.
		if op.Name() == group+"update" {
			if !c.IsSet(FlagName) && !c.IsSet(FlagMeta) && !c.IsSet(FlagClearMeta) {
				return fmt.Errorf("at least one field must be provided for update (--name, --meta, --clear-meta)")
			}
		}

		// Apply the configured per-command timeout.
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}
		return renderCatalogResult(ctx, c, op, result)
	}
}

// renderCatalogResult renders a handler's typed DATA result through the CLI
// Output formatter. It is the single rendering home for catalog-driven commands
// and never touches core services. It is a plain function invoked by
// catalogActionAdapter.
func renderCatalogResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *catalogops.DryRunResult:
		renderCatalogDryRun(output, c, r)
		return nil

	case []pinning.Pin:
		if len(r) == 0 {
			output.Printfln("No pins found")
			return nil
		}
		output.Printfln("Found %d pin(s)", len(r))
		rows := make([][]string, len(r))
		for i, p := range r {
			rows[i] = []string{p.CID, p.Name, p.Status, p.Created}
		}
		output.PrintTable([]string{"CID", "NAME", "STATUS", "CREATED"}, rows)
		return nil

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

// flagValue reads a compiled flag's value for the given operation arg into the
// input map, matching the catalog compiler's flag-to-input mapping so the
// adapter and compiler agree. Bool, Int, String, and StringSlice are covered;
// the pins domain uses only these.
func flagValue(c *cli.Command, a catalog.OperationArg) any {
	switch a.Type {
	case catalog.ArgTypeBool:
		return c.Bool(a.Name)
	case catalog.ArgTypeInt:
		return c.Int(a.Name)
	case catalog.ArgTypeStringSlice:
		return c.StringSlice(a.Name)
	default: // ArgTypeString
		return c.String(a.Name)
	}
}

// positionalCID returns the first positional argument, if any (used for
// the <cid> single-CID operations status/update).
func positionalCID(c *cli.Command) string {
	if c.Args().Len() == 0 {
		return ""
	}
	return c.Args().First()
}

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
// single-valued flags.
//
// The catalog compiler marks required OperationArgs as urfave-required, and
// urfave/cli/v3 fails at parse time when such a flag is not set, before any
// Action runs. But required args that the CLI passes positionally (for example
// `pins rm <cid>`) come through the wiring's positional-to-input mapping, not
// as a --<name> flag. If the marker stayed set, the command would be rejected
// before that mapping runs.
//
// Every catalog handler re-enforces requiredness itself (it errors when the
// resolved value is empty), so relaxing the marker loses no safety. It only
// lets positionally-supplied values reach the handler.
//
// Bool and StringSlice flags are untouched: Bool is never a positional target
// and StringSlice positional values are multi-valued and handled separately.
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
		}
	}
}
