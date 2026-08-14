package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// ipns_wiring.go adapts the IPNS catalog operations
// (internal/catalogops/ipns.go) to urfave/cli/v3 commands. The catalog never
// imports pkg/cli; this file maps CLI concerns (positional
// <key>/<id>/<cid>/<name> args, the destructive --force gate for key delete)
// onto the catalog and renders each handler's DATA result through the CLI
// Output formatter.
//
// The IPNS operations are canonically dotted ("ipns.keys.list",
// "ipns.publish", ...). The CLI nests ipns.keys.* under a "keys" parent; the
// rest are direct leaves. That nesting lives here, not in internal/catalog.

// catalogIPNSDeps builds the catalogops.IPNSDeps from the live CLI wiring.
func catalogIPNSDeps() catalogops.IPNSDeps {
	return catalogops.IPNSDeps{
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
		ServiceFactory: ipns.ServiceFactory,
		NewAuthenticated: func(cfgMgr config.Manager, token string, secure bool) (ipns.Service, error) {
			return ipns.NewAuthenticated(cfgMgr, token, secure)
		},
		GetAuthToken: func() string {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

var ipnsCatalogDepsVar = catalogops.IPNSDeps(catalogIPNSDeps())

// newIPNSCommandCatalog builds the catalog-driven "ipns" parent command. It
// compiles the IPNS operations and nests ipns.keys.* under a "keys" parent.
// newIPNSCommand in ipns.go delegates to this.
func newIPNSCommandCatalog() *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.IPNSOperations(ipnsCatalogDepsVar) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile ipns: %v", err))
	}

	parents := map[string]*cli.Command{}
	var out []*cli.Command
	for _, c := range compiled {
		canonical := c.Name // e.g. "ipns.keys.list", BEFORE mount mutates it
		mounted := mountIPNSCatalogCommand(c)
		rest := strings.TrimPrefix(canonical, "ipns_")
		if idx := strings.Index(rest, "_"); idx > 0 {
			// Two-level: parent_child (keys_list, keys_create, keys_get, keys_delete)
			parentName := rest[:idx]
			parent, ok := parents[parentName]
			if !ok {
				parent = &cli.Command{Name: parentName, Category: "Management", Usage: "Manage IPNS " + parentName, Commands: []*cli.Command{}}
				parents[parentName] = parent
				out = append(out, parent)
			}
			mounted.Name = rest[idx+1:]
			parent.Commands = append(parent.Commands, mounted)
			continue
		}
		out = append(out, mounted)
	}

	return &cli.Command{
		Name:        "ipns",
		Category:    "Management",
		Usage:       "Manage IPNS (InterPlanetary Name System) keys and records",
		Description: "Manage IPNS keys (create/list/get/delete), publish CIDs to IPNS names, republish records, and resolve IPNS names. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
		Commands:    out,
	}
}

// mountIPNSCatalogCommand adapts a single catalog-compiled command (dotted name
// like "ipns_keys_list") into a live ipns subcommand: it strips the "ipns_"
// group prefix, relaxes parsed-time required flags so positionals can supply
// them, and wraps the Action with the CLI-input adapter.
func mountIPNSCatalogCommand(cmd *cli.Command) *cli.Command {
	canonical := cmd.Name
	display := strings.TrimPrefix(canonical, "ipns_")
	cmd.Name = display
	cmd.Category = "Management"
	relaxFlagRequired(cmd)

	var op catalog.Operation
	for _, cand := range catalogops.IPNSOperations(ipnsCatalogDepsVar) {
		if cand.Name() == canonical {
			op = cand
			break
		}
	}
	if op != nil {
		cmd.Action = ipnsActionAdapter(op)
	}
	return cmd
}

// ipnsActionAdapter returns the per-invocation ActionFunc for an IPNS catalog
// operation. It maps the positional <key>/<id>/<cid>/<name> into the
// operation's string arg, threads the --auth-token override into the input,
// and invokes the handler, then renders the result.
func ipnsActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// The per-invocation --auth-token override takes precedence over the
		// config token. Put it in the input so IPNSDeps.service() honors it;
		// otherwise the config token would win and the flag would be ignored.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional arg into the operation's single string arg where
		// it is still empty (positional <key>/<id>/<cid>/<name>).
		if c.Args().Len() > 0 {
			for _, a := range op.Args() {
				if a.Type == catalog.ArgTypeString && catalog.StrArg(input, a.Name, "") == "" {
					input[a.Name] = c.Args().First()
					break
				}
			}
		}

		// The catalog marks ipns.keys.delete SafetyDestructive and the compiler
		// registers a --force flag for it, but the ipns keys delete command
		// deletes keys without requiring --force. To keep that contract (and not
		// break existing scripts), the CLI path does not gate on --force here.
		// The delete op's confirm arg defaults to true, so the normalize step
		// below fills confirm=true and the handler gate passes.

		// Route through the same normalize path as the generic adapter and
		// Catalog.Invoke so required-arg validation, declared defaults, and
		// SelectionGroup enforcement apply identically on the CLI surface.
		normalized, err := catalog.NormalizeOperationInput(op, input)
		if err != nil {
			return err
		}

		// Apply the configured per-command timeout.
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, normalized)
		if err != nil {
			return err
		}
		return renderIPNSResult(ctx, c, op, result)
	}
}

// renderIPNSResult renders an IPNS handler's typed DATA result through the CLI
// Output formatter. It is a plain function invoked by the wiring's own adapter.
func renderIPNSResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case []ipfs.IPNSKeyResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if len(r) == 0 {
			output.Printfln("No IPNS keys found.")
			return nil
		}
		headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
		rows := make([][]string, len(r))
		for i, k := range r {
			rows[i] = []string{
				fmt.Sprintf("%d", k.Id), k.Name, k.IpnsName, k.PeerId,
				k.Created.Format("2006-01-02 15:04:05"),
			}
		}
		output.PrintTable(headers, rows)
		return nil

	case *ipfs.IPNSKeyResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"ID", fmt.Sprintf("%d", r.Id)},
			{"Name", r.Name},
			{"IPNS Name", r.IpnsName},
			{"Peer ID", r.PeerId},
			{"Created", r.Created.Format("2006-01-02 15:04:05")},
		}})
		return nil

	case *ipfs.IPNSPublishResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"Name", r.Name},
			{"Value", r.Value},
			{"Sequence", fmt.Sprintf("%d", r.Sequence)},
			{"Published", r.Published.Format("2006-01-02 15:04:05")},
			{"Validity", r.Validity.Format("2006-01-02 15:04:05")},
		}})
		return nil

	case *ipfs.IPNSResolveResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"Path", r.Path},
			{"Value", r.Value},
		}})
		return nil

	case *ipfs.IPNSRepublishResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		// ipns.republish declares its key as the --key-name flag arg (not a
		// positional); the adapter also maps a positional <key> into it. Read
		// the flag first, falling back to positional args.
		keyArg := c.String("key-name")
		if keyArg == "" && c.Args().Len() > 0 {
			keyArg = c.Args().First()
		}
		output.Printfln("Republished IPNS key %s: %s (%d record(s))", keyArg, r.Message, r.Count)
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
