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
		Name: "api_keys_list", Title: "List API keys", Summary: "List all API keys",
		Description: "List all API keys for your account, optionally filtered by name.",
		Category:    "account", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{Name: "search", Type: catalog.ArgTypeString, Help: "Full-text search evaluated server-side against key name"},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			keys, _, err := svc.ListAPIKeys(ctx, catalog.SearchArg(input))
			if err != nil {
				return nil, err
			}
			page := catalog.ParseList(input)
			items := slicePage(keys, page.Start, page.Limit)
			headers := []string{"UUID", "NAME"}
			rows := make([][]string, 0, len(items))
			for _, k := range items {
				if k == nil {
					continue
				}
				rows = append(rows, []string{k.Uuid.String(), k.Name})
			}
			return NewListResult(items, ListResultMeta{
				Noun: "API key(s)", Headers: headers, Rows: rows,
			}), nil
		}),
	})
}

func apiKeysCreate(d APIKeysDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "api_keys_create", Title: "Create an API key", Summary: "Create a new API key",
		Description: "Create a new API key for your account. The created key value (the secret) is returned exactly once in the response: it cannot be retrieved again, only deleted and recreated. Treat it as a credential: store it securely, never log the full value, and revoke it (api_keys_delete) if exposed.",
		Category:    "account", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Key name"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("api_keys_create: key name is required")
			}
			return svc.CreateAPIKey(ctx, name)
		}),
	})
}

func apiKeysDelete(d APIKeysDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "api_keys_delete", Title: "Delete an API key", Summary: "Delete an API key",
		Description: "Delete an API key by name or UUID. Deleting the key currently used for authentication is blocked unless confirm=true.",
		Category:    "account", Safety: catalog.SafetyDestructive, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "API key name or UUID", AgentHelp: "The name or UUID of the API key to delete."},
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Help: "Allow deleting the key currently used for authentication", AgentHelp: "Set true to delete the API key even if it is the one currently used for authentication."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc := d.Service(input)
			if svc == nil {
				return nil, fmt.Errorf("api-keys service unavailable")
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("api_keys_delete: key name or UUID is required")
			}
			if err := svc.DeleteAPIKey(ctx, id, catalog.BoolArg(input, "confirm", false)); err != nil {
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
