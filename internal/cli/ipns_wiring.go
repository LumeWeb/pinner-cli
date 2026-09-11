package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/pinner/core/ipns"
)

// ipns_wiring.go adapts the IPNS catalog operations
// (internal/catalogops/ipns.go) to urfave/cli/v3 commands. The catalog never
// imports pkg/cli; this file maps CLI concerns (positional
// <key>/<id>/<cid>/<name> args, the destructive --force gate for key delete)
// onto the catalog and renders each handler's data result through the CLI
// Output formatter.
//
// The IPNS operations are canonically underscore-separated
// ("ipns_keys_list", "ipns_publish", ...). The CLI nests ipns_keys_* under a
// "keys" parent; the rest are direct leaves. That nesting is declared in
// internal/clicatalog/shapes_ipns.go, not hardcoded here.

// catalogIPNSDeps builds the catalogops.IPNSDeps from the live CLI wiring.
func catalogIPNSDeps(factory ...ConfigManagerFactory) catalogops.IPNSDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.IPNSDeps{
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
		ServiceFactory: ipns.ServiceFactory,
		NewAuthenticated: func(cfgMgr config.Manager, token string, secure bool) (ipns.Service, error) {
			return ipns.NewAuthenticated(cfgMgr, token, secure)
		},
		GetAuthToken: func() string {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

var ipnsCatalogDepsVar = catalogops.IPNSDeps(catalogIPNSDeps())

// newIPNSCommandCatalog builds the catalog-driven "ipns" parent command. It
// declares the IPNS operations' command shape in internal/clicatalog and
// materializes the tree through mount-owned leaf and parent builders.
// newIPNSCommand in ipns.go delegates to this.
func newIPNSCommandCatalog() *cli.Command {
	root := clicatalog.IPNSDomainRoot
	ops := catalogops.IPNSOperations(ipnsCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.IPNSShapes,
		root,
		ipnsCatalogConfig(),
		buildIPNSLeaf,
		buildCLIParent,
	)
	if err != nil {
		panic(fmt.Sprintf("catalog compile ipns: %v", err))
	}

	return &cli.Command{
		Name:        root.Name,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands:    cmds,
	}
}

// buildIPNSLeaf is the mount-owned leaf builder materializing one IPNS leaf
// into an urfave *cli.Command. Shape (name/category/aliases/flags/usage) comes
// from the clicatalog model via NewCLILeaf; behavior (the catalog action
// adapter + relaxFlagRequired) stays mount-owned here.
func buildIPNSLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// buildCLIParent is the mount-owned parent builder materializing a synthesized
// parent (e.g. ipns "keys") into an urfave *cli.Command wrapping its children.
// A FoldFlat leaf (ownLeaf) is folded into the parent's top-level Action, with
// its flags/usage/args-usage merged — reproducing the flat-leaf-into-parent
// fold. IPNS today has no folded leaf, so ownLeaf is nil here.
func buildCLIParent(spec clicatalog.NodeSpec, ownLeaf *cli.Command, children []*cli.Command) (*cli.Command, error) {
	parent := &cli.Command{
		Name: spec.Name, Category: spec.Category, Usage: spec.Usage,
		Description: spec.Desc, Aliases: spec.Aliases, Commands: children,
	}
	if ownLeaf != nil {
		parent.Action = ownLeaf.Action
		parent.Flags = append(parent.Flags, ownLeaf.Flags...)
		if parent.Usage == "" {
			parent.Usage = ownLeaf.Usage
		}
		parent.ArgsUsage = ownLeaf.ArgsUsage
	}
	return parent, nil
}

// ipnsCatalogConfig returns the CatalogAdapterConfig that expresses ipns'
// exact per-invocation behavior on top of the shared catalogActionAdapter
// pipeline. IPNS needs four hooks: the result renderer, the per-invocation
// --auth-token override, the positional <key>/<id>/<cid>/<name> mapping onto
// the first declared String/FlexibleID arg, and the deliberate no-gate for
// ipns keys delete (the op's confirm arg defaults to true and its handler
// carries the safety, so a confirm-less CLI delete still proceeds). All
// remaining fields stay nil so the shared pipeline's safe defaults apply.
func ipnsCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderIPNSResult,
		HonorAuthTokenOverride: true,

		// Map the positional <key>/<id>/<cid>/<name> into the first declared
		// String/FlexibleID arg when empty (ipns_keys_create name, keys_get/
		// keys_delete id, publish cid, resolve name). We use the type-based
		// posFirstToType mapping (not opmesh.MapPositionalArgs) because the
		// original adapter mapped a positional this way for every op —
		// including ipns_republish, whose canonical Positional is "" yet which
		// still accepted a positional into its key-name arg.
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			return posFirstToType(ic, func(a opmesh.OperationArg) bool {
				return a.Type == opmesh.ArgTypeString || a.Type == opmesh.ArgTypeFlexibleID
			})
		},

		// ipns keys delete uses NO destructive gate: the op's confirm arg
		// defaults to true and its handler enforces confirmation, so the CLI
		// must never refuse on a missing --force (that would break the
		// delete-without-force contract). GateNone writes no confirmation and
		// never handles, leaving the safety to the op's confirm default +
		// handler check.
		DestructiveGate: GateNone(),
	}
}

// renderIPNSResult renders an IPNS handler's typed data result through the CLI
// Output formatter. It is a plain function invoked by the wiring's own adapter.
func renderIPNSResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

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
