package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/operations"
)

// operations_wiring.go adapts the operations catalog operations
// (internal/catalogops/operations.go) to urfave/cli/v3 commands. It injects
// the concrete OperationsService and maps CLI concerns (positional <id>,
// rendering) onto the catalog.

// catalogOperationsDeps builds catalogops.OperationsDeps with a live
// OperationsService constructed per invocation (discard writer: handlers
// return pure data, all rendering happens in renderOperationsResult).
func catalogOperationsDeps(factory ...ConfigManagerFactory) catalogops.OperationsDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.OperationsDeps{
		Service: func(input map[string]any) operations.Service {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			discard := NewOutputFormatter(false, false, false, false)
			discard.SetWriter(io.Discard)
			// The per-invocation --auth-token flag (threaded through the input
			// map by operationsCatalogConfig (HonorAuthTokenOverride)) takes
			// precedence over the config
			// token.
			if t, ok := input[catalogops.AuthTokenInputKey].(string); ok && t != "" {
				authService := defaultAuthServiceFactoryWithToken(cfgMgr, cfgMgr.Config().GetAPIEndpoint(), t)
				return NewOperationsService(cfgMgr, discard, authService)
			}
			authService := defaultAuthServiceFactory(cfgMgr, cfgMgr.Config().GetAPIEndpoint())
			return NewOperationsService(cfgMgr, discard, authService)
		},
	}
}

var operationsCatalogDepsVar = catalogops.OperationsDeps(catalogOperationsDeps())

// newOperationsCommandCatalog is the catalog-driven "operations" parent
// command. (newOperationsCommand in operations.go delegates to this.) It
// declares the operations command shape in internal/clicatalog/shapes_operations.go
// and materializes the tree through mount-owned leaf and the shared parent
// builders.
func newOperationsCommandCatalog() *cli.Command {
	root := clicatalog.OperationsDomainRoot
	ops := catalogops.OperationsOperations(operationsCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.OperationsShapes,
		root,
		operationsCatalogConfig(),
		buildOperationsLeaf,
		buildCLIParent,
	)
	if err != nil {
		panic(fmt.Sprintf("catalog compile operations: %v", err))
	}

	return &cli.Command{
		Name:        root.Name,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands:    cmds,
	}
}

// buildOperationsLeaf is the mount-owned leaf builder materializing one
// operations leaf into an urfave *cli.Command. Shape (name/category/aliases/
// flags/usage) comes from the clicatalog model via NewCLILeaf; behavior (the
// catalog action adapter + relaxFlagRequired) stays mount-owned here.
func buildOperationsLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// operationsCatalogConfig returns the CatalogAdapterConfig that expresses
// operations' exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline. Operations needs three hooks: the result
// renderer, the per-invocation --auth-token override, and the positional <id>
// mapping onto the "id" arg. The `operations list --watch` polling loop runs
// the service directly (human-output only) and short-circuits BEFORE
// NormalizeOperationInput, so it lives here as a PreNormalizeWatch hook rather
// than in the pipeline's normalize/execute path. All remaining fields stay nil
// so the shared pipeline's safe defaults apply (operations ops are reads,
// never destructive, so no destructive gate runs).
func operationsCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderOperationsResult,
		HonorAuthTokenOverride: true,

		// Map the positional <id> into the operation's "id" arg when not already
		// provided. The id arg is ArgTypeInt, so FlagsToInput stores an int;
		// compare with IntArg (which coerces string/int) so an explicit --id
		// flag is not clobbered by a positional.
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			if ic.C.Args().Len() > 0 {
				if hasArg(ic.Op, "id") && opmesh.IntArg(ic.Input, "id", 0) == 0 {
					ic.Input["id"] = ic.C.Args().First()
				}
			}
			return nil
		},

		// `operations list --watch` polls until the list settles. Driving it
		// from the wiring (not the catalogops handler) keeps catalogops
		// IO-agnostic.
		PreNormalizeWatch: func(ic *CatalogInvokeContext) (bool, error) {
			if ic.Op.Name() == "operations_list" && ic.C.Bool(FlagWatch) && !ic.Output.IsJSON() {
				return true, watchCatalogOperationsList(ic.Ctx, ic.C, ic.Op, ic.Input)
			}
			return false, nil
		},
	}
}

// watchCatalogOperationsList runs the operations list watcher for the
// catalog-driven command, with an OperationsService resolved from the
// catalogops deps closure. It requires authentication up front and clamps
// page/pageSize to the defaults (1/10) so an unset or zero page-size does not
// disable pagination and fetch the entire operations table on every poll tick.
func watchCatalogOperationsList(ctx context.Context, c *cli.Command, op opmesh.Operation, input map[string]any) error {
	output := setupOutput(c)
	svc := operationsCatalogDepsVar.Service(input)
	if svc == nil {
		return fmt.Errorf("operations service unavailable")
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return err
	}
	l := opmesh.ParseList(input)
	// The watcher clamps to a sane default (Limit 10) so an unset or zero limit
	// does not disable pagination and fetch the entire operations table on
	// every poll tick.
	limit := l.Limit
	if limit < 1 {
		limit = 10
	}
	opts := operations.ListOptions{
		Search:          opmesh.SearchArg(input),
		StatusFilters:   opmesh.StrSliceArg(input, "status"),
		IncludeAll:      opmesh.BoolArg(input, "all", false),
		OperationFilter: opmesh.StrArg(input, "operation", ""),
		ProtocolFilter:  opmesh.StrArg(input, "protocol", ""),
		CIDFilter:       opmesh.StrArg(input, "cid", ""),
		Sort:            opmesh.StrArg(input, "sort", ""),
		Start:           l.Start,
		Limit:           limit,
	}
	opts.IsWatch = true
	return watchOperationsList(ctx, svc, output, opts)
}

// renderOperationsResult renders an operations handler's typed DATA through the
// CLI Output formatter.
func renderOperationsResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

	case *operations.OperationDetail:
		// Reuse the existing CLI human renderer (it accepts the alias type).
		return renderOperationDetail(output, r)

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
