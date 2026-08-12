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
		Name: "operations.list", Title: "List account operations", Summary: "List account operations",
		Description: "List account operations (uploads, pins, and other processing tasks) with optional filters and pagination.",
		Category:    "operations", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: []catalog.OperationArg{
			{Name: "status", Type: catalog.ArgTypeString, Help: "Filter by status (pending, running, completed, failed, error)"},
			{Name: "operation", Type: catalog.ArgTypeString, Help: "Filter by operation type (e.g. upload, pin)"},
			{Name: "protocol", Type: catalog.ArgTypeString, Help: "Filter by protocol (e.g. ipfs)"},
			{Name: "cid", Type: catalog.ArgTypeString, Help: "Filter by CID"},
			{Name: "sort", Type: catalog.ArgTypeString, Help: "Sort results (e.g. id:desc,started:asc)"},
			{Name: "page", Type: catalog.ArgTypeInt, Default: "0", Help: "Page number"},
			{Name: "page-size", Type: catalog.ArgTypeInt, Default: "0", Help: "Results per page"},
			{Name: "watch", Type: catalog.ArgTypeBool, Default: "false", Help: "Poll until the list settles"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("operations service unavailable")
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			page := catalog.IntArg(input, "page", 0)
			if page < 1 {
				page = 1
			}
			pageSize := catalog.IntArg(input, "page-size", 0)
			if pageSize < 1 {
				pageSize = 10
			}
			return svc.List(ctx, operations.ListOptions{
				StatusFilter:    catalog.StrArg(input, "status", ""),
				OperationFilter: catalog.StrArg(input, "operation", ""),
				ProtocolFilter:  catalog.StrArg(input, "protocol", ""),
				CIDFilter:       catalog.StrArg(input, "cid", ""),
				Sort:            catalog.StrArg(input, "sort", ""),
				Page:            page,
				PageSize:        pageSize,
			})
		}),
	})
}

func operationsGet(d OperationsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "operations.get", Title: "Get operation details", Summary: "Get details of an operation",
		Description: "Get the full details of a single account operation by ID, optionally waiting for it to complete.",
		Category:    "operations", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeInt, Required: true, Help: "Operation ID (positional)"},
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
				return nil, fmt.Errorf("operations.get: operation ID is required")
			}
			if catalog.BoolArg(input, "watch", false) {
				return svc.Watch(ctx, id)
			}
			return svc.Get(ctx, id)
		}),
	})
}
