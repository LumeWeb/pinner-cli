package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/operations"
)

// operations_wiring.go adapts the operations catalog operations
// (internal/catalogops/operations.go) to urfave/cli/v3 commands. It injects
// the concrete OperationsService and maps CLI concerns (positional <id>,
// rendering) onto the catalog.

// catalogOperationsDeps builds catalogops.OperationsDeps with a live
// OperationsService constructed per invocation (discard writer: handlers
// return pure data, all rendering happens in renderOperationsResult).
func catalogOperationsDeps() catalogops.OperationsDeps {
	return catalogops.OperationsDeps{
		Service: func(input map[string]any) operations.Service {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return nil
			}
			discard := NewOutputFormatter(false, false, false, false)
			discard.SetWriter(io.Discard)
			// The per-invocation --auth-token flag (threaded through the input
			// map by operationsActionAdapter) takes precedence over the config
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
// command. (newOperationsCommand in operations.go delegates to this.)
func newOperationsCommandCatalog() *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.OperationsOperations(operationsCatalogDepsVar) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile operations: %v", err))
	}

	out := make([]*cli.Command, 0, len(compiled))
	for _, c := range compiled {
		canonical := c.Name // e.g. "operations.list"
		c.Name = strings.TrimPrefix(canonical, "operations_")
		c.Category = "Management"
		relaxFlagRequired(c)

		var op catalog.Operation
		for _, cand := range catalogops.OperationsOperations(operationsCatalogDepsVar) {
			if cand.Name() == canonical {
				op = cand
				break
			}
		}
		if op != nil {
			c.Action = operationsActionAdapter(op)
		}
		out = append(out, c)
	}

	return &cli.Command{
		Name:        "operations",
		Category:    "Management",
		Usage:       "List and inspect account operations",
		Description: "View and monitor account operations such as uploads, pins, and other processing tasks. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
		Commands:    out,
	}
}

// operationsActionAdapter wraps a catalog operations command's Action: it maps
// the positional <id> into the operation input, then invokes the handler and
// renders the result.
func operationsActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {

		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// Thread the per-invocation --auth-token override into the operation
		// input so the Service closure honors it (flag over config).
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <id> into the operation's "id" arg when not already
		// provided. The id arg is ArgTypeInt, so flagValue stores an int; compare
		// with IntArg (which coerces string/int) so an explicit --id flag is not
		// clobbered by a positional.
		if c.Args().Len() > 0 {
			if hasArg(op, "id") && catalog.IntArg(input, "id", 0) == 0 {
				input["id"] = c.Args().First()
			}
		}

		// `operations list --watch` polls until the list settles. Driving it
		// from the wiring (not the catalogops handler) keeps catalogops
		// IO-agnostic.
		if op.Name() == "operations_list" && c.Bool(FlagWatch) && !setupOutput(c).IsJSON() {
			return watchCatalogOperationsList(ctx, c, op, input)
		}

		// Apply the legacy per-call deadline (shared with every catalog domain).
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}
		return renderOperationsResult(ctx, c, op, result)
	}
}

// watchCatalogOperationsList runs the operations list watcher for the
// catalog-driven command, with an OperationsService resolved from the
// catalogops deps closure. It requires authentication up front and clamps
// page/pageSize to the defaults (1/10) so an unset or zero page-size does not
// disable pagination and fetch the entire operations table on every poll tick.
func watchCatalogOperationsList(ctx context.Context, c *cli.Command, op catalog.Operation, input map[string]any) error {
	output := setupOutput(c)
	svc := operationsCatalogDepsVar.Service(input)
	if svc == nil {
		return fmt.Errorf("operations service unavailable")
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return err
	}
	page := catalog.IntArg(input, "page", 0)
	if page < 1 {
		page = 1
	}
	pageSize := catalog.IntArg(input, "page-size", 0)
	if pageSize < 1 {
		pageSize = 10
	}
	opts := operations.ListOptions{
		StatusFilter:    catalog.StrArg(input, "status", ""),
		OperationFilter: catalog.StrArg(input, "operation", ""),
		ProtocolFilter:  catalog.StrArg(input, "protocol", ""),
		CIDFilter:       catalog.StrArg(input, "cid", ""),
		Sort:            catalog.StrArg(input, "sort", ""),
		Page:            page,
		PageSize:        pageSize,
	}
	return watchOperationsList(ctx, svc, output, opts)
}

// renderOperationsResult renders an operations handler's typed DATA through the
// CLI Output formatter.
func renderOperationsResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *operations.OperationsListResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r == nil || len(r.Operations) == 0 {
			output.Printfln("No operations found.")
			return nil
		}
		headers := []string{"ID", "OPERATION", "PROTOCOL", "STATUS", "CID", "STARTED"}
		rows := make([][]string, len(r.Operations))
		for i, o := range r.Operations {
			cid := o.CID
			if cid == "" {
				cid = "-"
			}
			rows[i] = []string{
				fmt.Sprintf("%d", o.ID),
				o.OperationDisplayName,
				o.ProtocolDisplayName,
				o.StatusDisplayName,
				cid,
				o.StartedAt,
			}
		}
		output.PrintTable(headers, rows)
		return nil

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
