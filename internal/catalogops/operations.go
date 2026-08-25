// Package catalogops implements operations-domain operations for the
// operation catalog. catalogops depends only on the core OperationsService
// interface and injects the concrete service via deps.
package catalogops

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/operations"
)

// OperationsDeps injects an OperationsService. The concrete implementation
// lives in the pkg/cli frontend (NewOperationsService); catalogops only
// depends on its contract.
type OperationsDeps struct {
	// Service returns a live OperationsService for the current invocation,
	// honoring the per-invocation auth-token override in the input map.
	Service func(input map[string]any) operations.Service
}

// OperationsOperations returns the catalog operations for the operations
// domain (operations list, operations get).
func OperationsOperations(d OperationsDeps) []catalog.Operation {
	return []catalog.Operation{
		operationsList(d),
		operationsGet(d),
	}
}

func operationsList(d OperationsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "operations_list", Title: "List account operations", Summary: "List account operations",
		Description: "List account operations (uploads, pins, and other processing tasks) with optional filters and pagination. By default only active operations (pending, processing) are shown; pass --all to include completed, failed, and duplicate operations.",
		Category:    "operations", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{Name: "search", Type: catalog.ArgTypeString, Help: "Full-text search evaluated server-side against operation type, status, protocol, or CID; composes with the filters below", AgentHelp: "Full-text search term evaluated server-side against operation type, status, protocol, or CID. Composes (AND) with the structured filters."},
			catalog.OperationArg{Name: "status", Type: catalog.ArgTypeStringSlice, Help: "Filter by status (repeatable; pending, processing, completed, failed, duplicate)", AgentHelp: "One or more statuses to filter by. Valid values: pending, processing, completed, failed, duplicate. When omitted, only active operations (pending, processing) are returned unless all=true."},
			catalog.OperationArg{Name: "all", Type: catalog.ArgTypeBool, Default: "false", Help: "Show operations in all statuses (overrides the default active-only filter)", AgentHelp: "When true, return operations in any status, overriding the default that shows only pending and processing. Ignored when status is explicitly provided."},
			catalog.OperationArg{Name: "operation", Type: catalog.ArgTypeString, Help: "Filter by operation type (e.g. upload, pin)"},
			catalog.OperationArg{Name: "protocol", Type: catalog.ArgTypeString, Help: "Filter by protocol (e.g. ipfs)"},
			catalog.OperationArg{Name: "cid", Type: catalog.ArgTypeString, Help: "Filter by CID"},
			catalog.OperationArg{Name: "sort", Type: catalog.ArgTypeString, Help: "Sort results (e.g. id:desc, started:asc). Defaults to id:desc.", AgentHelp: "Sort field and direction, e.g. \"id:desc\" or \"started:asc\". Defaults to id:desc."},
			catalog.OperationArg{Name: "watch", Type: catalog.ArgTypeBool, Default: "false", Help: "Poll until the list settles"},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("operations service unavailable")
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			page := catalog.ParseList(input)
			// Operations is the one list that pages server-side over a
			// potentially-large, long-running table (every historical upload,
			// pin, etc.). To avoid an unbounded full-table fetch on a plain
			// `operations list`, default a zero/absent --limit to one page —
			// the same default the watch path (watchCatalogOperationsList)
			// enforces. Callers set --limit explicitly to page or go big.
			if page.Limit < 1 {
				page.Limit = 10
			}
			res, err := svc.List(ctx, operations.ListOptions{
				Search:          catalog.SearchArg(input),
				StatusFilters:   catalog.StrSliceArg(input, "status"),
				IncludeAll:      catalog.BoolArg(input, "all", false),
				OperationFilter: catalog.StrArg(input, "operation", ""),
				ProtocolFilter:  catalog.StrArg(input, "protocol", ""),
				CIDFilter:       catalog.StrArg(input, "cid", ""),
				Sort:            catalog.StrArg(input, "sort", ""),
				Start:           page.Start,
				Limit:           page.Limit,
			})
			if err != nil {
				return nil, err
			}
			return newOperationsListResult(res), nil
		}),
	})
}

// newOperationsListResult wraps the core operations list result into the
// shared ListResult contract so the CLI and MCP surfaces render it uniformly.
func newOperationsListResult(res *operations.OperationsListResult) ListResult {
	headers := []string{"ID", "OPERATION", "PROTOCOL", "STATUS", "CID", "STARTED"}
	if res == nil {
		return NewListResult([]operations.OperationListItem{}, ListResultMeta{
			Noun: "operation(s)", Headers: headers,
		})
	}
	rows := make([][]string, 0, len(res.Operations))
	for _, o := range res.Operations {
		cid := o.CID
		if cid == "" {
			cid = "-"
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", o.ID),
			o.OperationDisplayName,
			o.ProtocolDisplayName,
			o.StatusDisplayName,
			cid,
			o.StartedAt,
		})
	}
	return NewListResult(res.Operations, ListResultMeta{
		Noun: "operation(s)", Headers: headers, Rows: rows, Total: res.Total,
	})
}

func operationsGet(d OperationsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "operations_get", Title: "Get operation details", Summary: "Get details of an operation",
		Description: "Get the full details of a single account operation by ID, optionally waiting for it to complete.",
		Category:    "operations", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeInt, Required: true, Help: "Operation ID"},
			{Name: "watch", Type: catalog.ArgTypeBool, Default: "false", Help: "Wait for the operation to complete"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("operations service unavailable")
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := int64(catalog.IntArg(input, "id", 0))
			if id == 0 {
				return nil, fmt.Errorf("operations_get: operation ID is required")
			}
			if catalog.BoolArg(input, "watch", false) {
				return svc.Watch(ctx, id)
			}
			return svc.Get(ctx, id)
		}),
	})
}
