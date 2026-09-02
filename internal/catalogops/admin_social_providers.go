package catalogops

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/portal-sdk/admin"
)

// socialProvidersListResult builds the shared ListResult view for the social
// providers list operation.
func socialProvidersListResult(providers []*admin.SocialProvider) ListResult {
	headers := []string{"ID", "PROVIDER", "DISPLAY NAME", "ENABLED", "ORDER"}
	return NewListResult(providers, ListResultMeta{Noun: "social provider(s)", Headers: headers, Rows: socialProviderRows(providers)})
}

// socialProvidersListResultTotal is socialProvidersListResult with the backend
// total attached (for list pagination display).
func socialProvidersListResultTotal(providers []*admin.SocialProvider, total int) ListResult {
	return NewListResult(providers, ListResultMeta{Noun: "social provider(s)", Headers: []string{"ID", "PROVIDER", "DISPLAY NAME", "ENABLED", "ORDER"}, Rows: socialProviderRows(providers), Total: total})
}

func socialProviderRows(providers []*admin.SocialProvider) [][]string {
	rows := make([][]string, 0, len(providers))
	for _, p := range providers {
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), p.ProviderId, p.DisplayName,
			adminYesNo(p.Enabled), fmt.Sprintf("%d", p.OrderIndex),
		})
	}
	return rows
}

// SocialProvidersDeleteResult reports a deleted social provider.
type SocialProvidersDeleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// socialProviderRequestFromInput builds a SocialProviderRequest from the op
// input. Required identity fields are validated by the caller so the handler
// can relax the CLI Required markers for positionals.
func socialProviderRequestFromInput(input map[string]any) *admin.SocialProviderRequest {
	return &admin.SocialProviderRequest{
		ProviderId:   catalog.StrArg(input, "provider-id", ""),
		ClientId:     catalog.StrArg(input, "client-id", ""),
		ClientSecret: catalog.StrArg(input, "client-secret", ""),
		DisplayName:  catalog.StrArg(input, "display-name", ""),
		AuthUrl:      catalog.StrArg(input, "auth-url", ""),
		TokenUrl:     catalog.StrArg(input, "token-url", ""),
		UserUrl:      catalog.StrArg(input, "user-url", ""),
		Scopes:       catalog.StrSliceArg(input, "scopes"),
		UserIdKey:    catalog.StrArg(input, "user-id-key", ""),
		UserEmailKey: catalog.StrArg(input, "user-email-key", ""),
		UserNameKey:  catalog.StrArg(input, "user-name-key", ""),
		OrderIndex:   catalog.IntArg(input, "order-index", 0),
		Enabled:      catalog.BoolArg(input, "enabled", false),
	}
}

// adminSocialProvidersList is the `admin social-providers list` operation.
func adminSocialProvidersList(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_list",
		Title:       "List social providers",
		Summary:     "List all social login providers",
		Description: "List all configured social login providers. Client secrets are never returned. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args:        catalog.ListArgs(),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			providers, total, err := svc.ListSocialProviders(ctx)
			if err != nil {
				return nil, err
			}
			page := catalog.ParseList(input)
			paged := slicePage(providers, page.Start, page.Limit)
			return socialProvidersListResultTotal(paged, total), nil
		}),
	})
}

// adminSocialProvidersGet is the `admin social-providers get` operation.
func adminSocialProvidersGet(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_get",
		Title:       "Get a social provider",
		Summary:     "Get a social login provider by ID",
		Description: "Get a single social login provider by numeric ID. Client secrets are never returned. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_social_providers_get: provider ID is required")
			}
			return svc.GetSocialProvider(ctx, id)
		}),
	})
}

// adminSocialProvidersCreate is the `admin social-providers create` operation.
func adminSocialProvidersCreate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_create",
		Title:       "Create a social provider",
		Summary:     "Create a social login provider configuration",
		Description: "Create a new social login provider configuration (OAuth2 endpoints, client credentials, attribute keys and display metadata). Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "provider-id", Type: catalog.ArgTypeString, Required: true, Help: "Provider type identifier (e.g. github, google)"},
			{Name: "client-id", Type: catalog.ArgTypeString, Required: true, Help: "OAuth2 client ID"},
			{Name: "client-secret", Type: catalog.ArgTypeString, Required: true, Help: "OAuth2 client secret"},
			{Name: "display-name", Type: catalog.ArgTypeString, Required: true, Help: "Human-readable provider name"},
			{Name: "auth-url", Type: catalog.ArgTypeString, Required: true, Help: "OAuth2 authorization endpoint"},
			{Name: "token-url", Type: catalog.ArgTypeString, Required: true, Help: "OAuth2 token endpoint"},
			{Name: "user-url", Type: catalog.ArgTypeString, Required: true, Help: "User info endpoint"},
			{Name: "scopes", Type: catalog.ArgTypeStringSlice, Help: "OAuth2 scopes to request"},
			{Name: "user-id-key", Type: catalog.ArgTypeString, Required: true, Help: "User info JSON key holding the user ID"},
			{Name: "user-email-key", Type: catalog.ArgTypeString, Required: true, Help: "User info JSON key holding the email"},
			{Name: "user-name-key", Type: catalog.ArgTypeString, Required: true, Help: "User info JSON key holding the display name"},
			{Name: "order-index", Type: catalog.ArgTypeInt, Help: "Display order (lower first)"},
			{Name: "enabled", Type: catalog.ArgTypeBool, Help: "Enable the provider for login"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			req := socialProviderRequestFromInput(input)
			if req.ProviderId == "" {
				return nil, fmt.Errorf("admin_social_providers_create: provider-id is required")
			}
			if req.ClientId == "" {
				return nil, fmt.Errorf("admin_social_providers_create: client-id is required")
			}
			if req.ClientSecret == "" {
				return nil, fmt.Errorf("admin_social_providers_create: client-secret is required")
			}
			if req.DisplayName == "" {
				return nil, fmt.Errorf("admin_social_providers_create: display-name is required")
			}
			return svc.CreateSocialProvider(ctx, req)
		}),
	})
}

// adminSocialProvidersUpdate is the `admin social-providers update` operation.
func adminSocialProvidersUpdate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:    "admin_social_providers_update",
		Title:   "Update a social provider",
		Summary: "Update a social login provider configuration",
		// Nullable arg types matter for updates: omitted must be distinguishable
		// from the zero value, else an update without the enabled flag would
		// arrive as enabled=false and the backend would disable the provider.
		Description: "Update an existing social login provider configuration. Only the fields provided are changed; others keep their current values (an omitted client-secret keeps the stored secret). Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
			{Name: "client-secret", Type: catalog.ArgTypeString, Help: "OAuth2 client secret (omit to keep the stored secret)"},
			{Name: "provider-key", Type: catalog.ArgTypeString, Help: "Provider type identifier (e.g. github, google)"},
			{Name: "client-id", Type: catalog.ArgTypeString, Help: "OAuth2 client ID"},
			{Name: "display-name", Type: catalog.ArgTypeString, Help: "Human-readable provider name"},
			{Name: "auth-url", Type: catalog.ArgTypeString, Help: "OAuth2 authorization endpoint"},
			{Name: "token-url", Type: catalog.ArgTypeString, Help: "OAuth2 token endpoint"},
			{Name: "user-url", Type: catalog.ArgTypeString, Help: "User info endpoint"},
			{Name: "scopes", Type: catalog.ArgTypeStringSlice, Help: "OAuth2 scopes to request (replaces the current set; omit to keep it)"},
			{Name: "user-id-key", Type: catalog.ArgTypeString, Help: "User info JSON key holding the user ID"},
			{Name: "user-email-key", Type: catalog.ArgTypeString, Help: "User info JSON key holding the email"},
			{Name: "user-name-key", Type: catalog.ArgTypeString, Help: "User info JSON key holding the display name"},
			{Name: "order-index", Type: catalog.ArgTypeNullableInt, Help: "Display order (lower first)"},
			{Name: "enabled", Type: catalog.ArgTypeNullableBool, Help: "Enable the provider for login"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_social_providers_update: provider ID is required")
			}
			// Nil fields are sent omitted and the backend leaves them unchanged.
			req := &admin.SocialProviderUpdateRequest{}
			if v := catalog.StrArg(input, "provider-key", ""); v != "" {
				req.ProviderId = &v
			}
			if v := catalog.StrArg(input, "client-id", ""); v != "" {
				req.ClientId = &v
			}
			if v := catalog.StrArg(input, "client-secret", ""); v != "" {
				req.ClientSecret = &v
			}
			if v := catalog.StrArg(input, "display-name", ""); v != "" {
				req.DisplayName = &v
			}
			if v := catalog.StrArg(input, "auth-url", ""); v != "" {
				req.AuthUrl = &v
			}
			if v := catalog.StrArg(input, "token-url", ""); v != "" {
				req.TokenUrl = &v
			}
			if v := catalog.StrArg(input, "user-url", ""); v != "" {
				req.UserUrl = &v
			}
			// An omitted slice arg normalizes to a non-nil empty []string, so
			// presence is judged by length: forwarding that empty slice on the
			// patch would erase all scopes a provider still needs.
			if v := catalog.StrSliceArg(input, "scopes"); len(v) > 0 {
				req.Scopes = &v
			}
			if v := catalog.StrArg(input, "user-id-key", ""); v != "" {
				req.UserIdKey = &v
			}
			if v := catalog.StrArg(input, "user-email-key", ""); v != "" {
				req.UserEmailKey = &v
			}
			if v := catalog.StrArg(input, "user-name-key", ""); v != "" {
				req.UserNameKey = &v
			}
			if v := catalog.IntArgPtr(input, "order-index"); v != nil {
				req.OrderIndex = v
			}
			if v := catalog.BoolArgPtr(input, "enabled"); v != nil {
				req.Enabled = v
			}
			return svc.UpdateSocialProvider(ctx, id, req)
		}),
	})
}

// adminSocialProvidersDelete is the `admin social-providers delete` operation.
// DESTRUCTIVE: requires confirm=true.
func adminSocialProvidersDelete(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_delete",
		Title:       "Delete a social provider",
		Summary:     "Delete a social login provider by ID",
		Description: "Delete a social login provider configuration by ID. DESTRUCTIVE: users will no longer be able to sign in with this provider. Requires confirm=true. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("admin_social_providers_delete: confirmation is required")
			}
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_social_providers_delete: provider ID is required")
			}
			if err := svc.DeleteSocialProvider(ctx, id); err != nil {
				return nil, err
			}
			return &SocialProvidersDeleteResult{Deleted: true, ID: id}, nil
		}),
	})
}

// adminSocialProvidersEnable is the `admin social-providers enable` operation.
func adminSocialProvidersEnable(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_enable",
		Title:       "Enable a social provider",
		Summary:     "Enable a social login provider",
		Description: "Enable a previously disabled social login provider so users can sign in with it. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_social_providers_enable: provider ID is required")
			}
			return svc.EnableSocialProvider(ctx, id)
		}),
	})
}

// adminSocialProvidersDisable is the `admin social-providers disable`
// operation.
func adminSocialProvidersDisable(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_social_providers_disable",
		Title:       "Disable a social provider",
		Summary:     "Disable a social login provider",
		Description: "Disable a social login provider so it can no longer be used to authenticate. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.socialProviders()
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_social_providers_disable: provider ID is required")
			}
			return svc.DisableSocialProvider(ctx, id)
		}),
	})
}
