// Package catalogops implements API-key domain operations for the operation
// catalog, driving the core apikeys service directly.
package catalogops

import (
	"context"
	"fmt"

	portalsdk "go.lumeweb.com/portal-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/apikeys"
)

// APIKeysDeps injects the dependencies for building an apikeys.Service.
type APIKeysDeps struct {
	// Service returns a live apikeys.Service for the current invocation,
	// honoring the per-invocation auth-token override in the input map.
	Service func(input map[string]any) apikeys.Service
}

// APIKeysOperations returns the catalog operations for the api-keys domain
// (list, create, delete).
func APIKeysOperations(d APIKeysDeps) []catalog.Operation {
	return []catalog.Operation{
		apiKeysList(d),
		apiKeysCreate(d),
		apiKeysDelete(d),
	}
}

func apiKeysList(d APIKeysDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "api-keys.list", Title: "List API keys", Summary: "List all API keys",
		Description: "List all API keys for your account, optionally filtered by name.",
		Category:    "account", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: []catalog.OperationArg{
			{Name: "search", Type: catalog.ArgTypeString, Help: "Search API keys by name"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			keys, total, err := svc.ListAPIKeys(ctx, catalog.StrArg(input, "search", ""))
			if err != nil {
				return nil, err
			}
			return &APIKeysListResult{Keys: keys, Total: total}, nil
		}),
	})
}

func apiKeysCreate(d APIKeysDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "api-keys.create", Title: "Create an API key", Summary: "Create a new API key",
		Description: "Create a new API key for your account.",
		Category:    "account", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Key name (positional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("api-keys.create: key name is required")
			}
			return svc.CreateAPIKey(ctx, name)
		}),
	})
}

func apiKeysDelete(d APIKeysDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "api-keys.delete", Title: "Delete an API key", Summary: "Delete an API key",
		Description: "Delete an API key by name or UUID. Blocked if the key is the one currently used for auth unless --force.",
		Category:    "account", Safety: catalog.SafetyDestructive, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "API key name or UUID (positional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("api-keys.delete: key name or UUID is required")
			}
			if err := svc.DeleteAPIKey(ctx, id, catalog.BoolArg(input, "force", false)); err != nil {
				return nil, err
			}
			return &APIKeyDeleteResult{ID: id}, nil
		}),
	})
}

// APIKeysListResult wraps the raw []*portalsdk.APIKey + total from the core
// service so the frontend can render a typed result.
type APIKeysListResult struct {
	Keys  []*portalsdk.APIKey `json:"keys"`
	Total int                 `json:"total"`
}

// APIKeyDeleteResult reports a successful API key deletion (or a self-delete
// that was explicitly forced).
type APIKeyDeleteResult struct {
	ID      string `json:"id"`
	Message string `json:"message,omitempty"`
}
