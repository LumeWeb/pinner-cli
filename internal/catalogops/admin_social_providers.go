package catalogops

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/portal-sdk/admin"
)

// SocialProvidersListResult wraps the ([]*admin.SocialProvider, total, error)
// result.
type SocialProvidersListResult struct {
	Count     int                      `json:"count"`
	Providers []*admin.SocialProvider  `json:"providers"`
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
			providers, _, err := svc.ListSocialProviders(ctx)
			if err != nil {
				return nil, err
			}
			page := catalog.ParseList(input)
			return SocialProvidersListResult{Count: len(slicePage(providers, page.Start, page.Limit)), Providers: slicePage(providers, page.Start, page.Limit)}, nil
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
		// The backend update is a full-replace PUT and the API never returns the
		// client secret, so there is no way to merge "keep the current secret".
		// The secret must be re-supplied on every update; read-visible fields are
		// merged from the existing provider when omitted.
		Description: "Update an existing social login provider configuration. Client secrets are never returned by the API, so client-secret is required on every update even when unchanged. Fields not supplied keep their current values. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<provider-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Social provider ID", PositionalOnly: true},
			{Name: "client-secret", Type: catalog.ArgTypeString, Required: true, Help: "OAuth2 client secret (required; the API never returns it)"},
			{Name: "provider-key", Type: catalog.ArgTypeString, Help: "Provider type identifier (e.g. github, google)"},
			{Name: "client-id", Type: catalog.ArgTypeString, Help: "OAuth2 client ID"},
			{Name: "display-name", Type: catalog.ArgTypeString, Help: "Human-readable provider name"},
			{Name: "auth-url", Type: catalog.ArgTypeString, Help: "OAuth2 authorization endpoint"},
			{Name: "token-url", Type: catalog.ArgTypeString, Help: "OAuth2 token endpoint"},
			{Name: "user-url", Type: catalog.ArgTypeString, Help: "User info endpoint"},
			{Name: "scopes", Type: catalog.ArgTypeStringSlice, Help: "OAuth2 scopes to request"},
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
			secret := catalog.StrArg(input, "client-secret", "")
			if secret == "" {
				return nil, fmt.Errorf("admin_social_providers_update: client-secret is required (the API never returns the stored secret)")
			}
			existing, err := svc.GetSocialProvider(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to get existing provider: %w", err)
			}
			// Full-replace PUT: fill any un-supplied read-visible field from the
			// existing provider so the CLI's always-filled args can't zero them.
			req := &admin.SocialProviderRequest{
				ProviderId:   existing.ProviderId,
				ClientId:     existing.ClientId,
				ClientSecret: secret,
				DisplayName:  existing.DisplayName,
				AuthUrl:      existing.AuthUrl,
				TokenUrl:     existing.TokenUrl,
				UserUrl:      existing.UserUrl,
				Scopes:       existing.Scopes,
				UserIdKey:    existing.UserIdKey,
				UserEmailKey: existing.UserEmailKey,
				UserNameKey:  existing.UserNameKey,
				OrderIndex:   existing.OrderIndex,
				Enabled:      existing.Enabled,
			}
			if v := catalog.StrArg(input, "provider-key", ""); v != "" {
				req.ProviderId = v
			}
			if v := catalog.StrArg(input, "client-id", ""); v != "" {
				req.ClientId = v
			}
			if v := catalog.StrArg(input, "display-name", ""); v != "" {
				req.DisplayName = v
			}
			if v := catalog.StrArg(input, "auth-url", ""); v != "" {
				req.AuthUrl = v
			}
			if v := catalog.StrArg(input, "token-url", ""); v != "" {
				req.TokenUrl = v
			}
			if v := catalog.StrArg(input, "user-url", ""); v != "" {
				req.UserUrl = v
			}
			if v := catalog.StrSliceArg(input, "scopes"); v != nil {
				req.Scopes = v
			}
			if v := catalog.StrArg(input, "user-id-key", ""); v != "" {
				req.UserIdKey = v
			}
			if v := catalog.StrArg(input, "user-email-key", ""); v != "" {
				req.UserEmailKey = v
			}
			if v := catalog.StrArg(input, "user-name-key", ""); v != "" {
				req.UserNameKey = v
			}
			if v := catalog.IntArgPtr(input, "order-index"); v != nil {
				req.OrderIndex = *v
			}
			if v := catalog.BoolArgPtr(input, "enabled"); v != nil {
				req.Enabled = *v
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
