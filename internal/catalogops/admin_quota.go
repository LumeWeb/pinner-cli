package catalogops

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/portal-sdk/admin"
)

// QuotaPlansListResult wraps the ([]*admin.QuotaPlan, total, error) result.
type QuotaPlansListResult struct {
	Count int                 `json:"count"`
	Plans []*admin.QuotaPlan  `json:"plans"`
}

// QuotaAllowancesListResult wraps the allowances list result.
type QuotaAllowancesListResult struct {
	Count          int                     `json:"count"`
	Allowances     []*admin.QuotaAllowance `json:"allowances"`
}

// QuotaUserConfigsListResult wraps the user configs list result.
type QuotaUserConfigsListResult struct {
	Count   int                        `json:"count"`
	Configs []*admin.UserQuotaConfig   `json:"configs"`
}

// QuotaPlansDeleteResult reports a deleted plan.
type QuotaPlansDeleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// QuotaPlansSetDefaultResult reports a plan set as default, plus the default
// flag for renderers.
type QuotaPlansSetDefaultResult struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"is_default"`
}

// QuotaAllowancesDeleteResult reports a deleted allowance.
type QuotaAllowancesDeleteResult struct {
	Deleted bool   `json:"deleted"`
	GrantID string `json:"grant_id"`
}

// QuotaUserConfigsResetResult reports a reset user config.
type QuotaUserConfigsResetResult struct {
	UserID int  `json:"user_id"`
	Reset  bool `json:"reset"`
}

// QuotaReconcileResult reports the reconcile outcome.
type QuotaReconcileResult struct {
	Message        string `json:"message"`
	UsersProcessed int    `json:"users_processed"`
}

// QuotaCleanupResult reports the number of expired records cleaned up.
type QuotaCleanupResult struct {
	Deleted int `json:"deleted"`
}

// derivedLimit reads an optional int arg (nil when omitted) and returns its
// value or fallback.
func derivedLimit(input map[string]any, key string, fallback int) int {
	if v := catalog.IntArgPtr(input, key); v != nil {
		return *v
	}
	return fallback
}

// adminQuotaPlansList is the `admin quota plans list` operation.
func adminQuotaPlansList(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_list",
		Title:       "List quota plans",
		Summary:     "List all quota plans",
		Description: "List all quota plans with their upload/download/storage limits and active/default flags. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			plans, total, err := svc.ListPlans(ctx)
			if err != nil {
				return nil, err
			}
			return &QuotaPlansListResult{Count: total, Plans: plans}, nil
		}),
	})
}

// adminQuotaPlansGet is the `admin quota plans get` operation.
func adminQuotaPlansGet(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_get",
		Title:       "Get a quota plan",
		Summary:     "Get a quota plan by ID",
		Description: "Get a single quota plan by numeric ID. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<plan-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Quota plan ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_quota_plans_get: plan ID is required")
			}
			return svc.GetPlan(ctx, id)
		}),
	})
}

// adminQuotaPlansCreate is the `admin quota plans create` operation.
func adminQuotaPlansCreate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_create",
		Title:       "Create a quota plan",
		Summary:     "Create a quota plan",
		Description: "Create a quota plan with upload/download/storage limits and an optional window type (default LIFETIME). Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Plan name"},
			{Name: "description", Type: catalog.ArgTypeString, Help: "Plan description"},
			{Name: "upload-limit", Type: catalog.ArgTypeInt, Help: "Upload limit (bytes)"},
			{Name: "download-limit", Type: catalog.ArgTypeInt, Help: "Download limit (bytes)"},
			{Name: "storage-limit", Type: catalog.ArgTypeInt, Help: "Storage limit (bytes)"},
			{Name: "window-type", Type: catalog.ArgTypeString, Default: "LIFETIME", Help: "Window type (ROLLING, DAY, WEEK, MONTH, YEAR, LIFETIME)"},
			{Name: "is-active", Type: catalog.ArgTypeBool, Help: "Mark plan as active"},
			{Name: "is-default", Type: catalog.ArgTypeBool, Help: "Set as default plan for new users"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("admin_quota_plans_create: name is required")
			}
			windowType := catalog.StrArg(input, "window-type", "LIFETIME")
			limits := admin.QuotaLimits{
				UploadLimitBytes:   catalog.IntArg(input, "upload-limit", 0),
				DownloadLimitBytes: catalog.IntArg(input, "download-limit", 0),
				StorageLimitBytes:  catalog.IntArg(input, "storage-limit", 0),
				WindowType:         windowType,
			}
			plan := admin.NewQuotaPlan(name, catalog.StrArg(input, "description", ""), limits)
			plan.IsActive = catalog.BoolArg(input, "is-active", false)
			created, err := svc.CreatePlan(ctx, plan)
			if err != nil {
				return nil, err
			}
			if catalog.BoolArg(input, "is-default", false) {
				if err := svc.SetDefaultPlan(ctx, fmt.Sprintf("%d", created.Id)); err != nil {
					return nil, fmt.Errorf("plan created but failed to set as default: %w", err)
				}
				created.IsDefault = true
			}
			return created, nil
		}),
	})
}

// adminQuotaPlansUpdate is the `admin quota plans update` operation. Only the
// fields provided are overridden; others keep their current values.
func adminQuotaPlansUpdate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_update",
		Title:       "Update a quota plan",
		Summary:     "Update a quota plan",
		Description: "Update an existing quota plan. Only the fields provided are changed; others keep their current values. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<plan-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Quota plan ID", PositionalOnly: true},
			{Name: "name", Type: catalog.ArgTypeString, Help: "Plan name"},
			{Name: "description", Type: catalog.ArgTypeString, Help: "Plan description"},
			{Name: "upload-limit", Type: catalog.ArgTypeNullableInt, Help: "Upload limit (bytes)"},
			{Name: "download-limit", Type: catalog.ArgTypeNullableInt, Help: "Download limit (bytes)"},
			{Name: "storage-limit", Type: catalog.ArgTypeNullableInt, Help: "Storage limit (bytes)"},
			{Name: "window-type", Type: catalog.ArgTypeString, Help: "Window type (ROLLING, DAY, WEEK, MONTH, YEAR, LIFETIME)"},
			{Name: "is-active", Type: catalog.ArgTypeNullableBool, Help: "Mark plan as active"},
			{Name: "is-default", Type: catalog.ArgTypeNullableBool, Help: "Set as default plan for new users"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			planID := catalog.StrArg(input, "id", "")
			if planID == "" {
				return nil, fmt.Errorf("admin_quota_plans_update: plan ID is required")
			}
			if !hasAny(input, "name", "description", "upload-limit", "download-limit", "storage-limit", "window-type", "is-active", "is-default") {
				return nil, fmt.Errorf("admin_quota_plans_update: at least one field is required")
			}
			existing, err := svc.GetPlan(ctx, planID)
			if err != nil {
				return nil, fmt.Errorf("failed to get existing plan: %w", err)
			}
			limits := admin.QuotaLimits{
				UploadLimitBytes:   existing.UploadLimitBytes,
				DownloadLimitBytes: existing.DownloadLimitBytes,
				StorageLimitBytes:  existing.StorageLimitBytes,
				WindowType:         existing.WindowType,
				WindowDuration:     existing.WindowDuration,
				WindowStartHour:    existing.WindowStartHour,
				WindowTimezone:     existing.WindowTimezone,
			}
			limits.UploadLimitBytes = derivedLimit(input, "upload-limit", limits.UploadLimitBytes)
			limits.DownloadLimitBytes = derivedLimit(input, "download-limit", limits.DownloadLimitBytes)
			limits.StorageLimitBytes = derivedLimit(input, "storage-limit", limits.StorageLimitBytes)
			if wt := catalog.StrArg(input, "window-type", ""); wt != "" {
				limits.WindowType = wt
			}
			name := existing.Name
			if n := catalog.StrArg(input, "name", ""); n != "" {
				name = n
			}
			description := existing.Description
			if dsc := catalog.StrArg(input, "description", ""); dsc != "" {
				description = dsc
			}
			plan := admin.NewQuotaPlan(name, description, limits)
			plan.IsActive = existing.IsActive
			if v := catalog.BoolArgPtr(input, "is-active"); v != nil {
				plan.IsActive = *v
			}
			updated, err := svc.UpdatePlan(ctx, planID, plan)
			if err != nil {
				return nil, err
			}
			if v := catalog.BoolArgPtr(input, "is-default"); v != nil && *v {
				if err := svc.SetDefaultPlan(ctx, planID); err != nil {
					return nil, fmt.Errorf("plan updated but failed to set as default: %w", err)
				}
				updated.IsDefault = true
			}
			return updated, nil
		}),
	})
}

// adminQuotaPlansDelete is the `admin quota plans delete` operation.
// DESTRUCTIVE: requires confirm=true.
func adminQuotaPlansDelete(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_delete",
		Title:       "Delete a quota plan",
		Summary:     "Delete a quota plan by ID",
		Description: "Delete a quota plan by numeric ID. DESTRUCTIVE and irreversible: requires confirm=true. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<plan-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Quota plan ID", PositionalOnly: true},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("admin_quota_plans_delete: confirmation is required")
			}
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_quota_plans_delete: plan ID is required")
			}
			if err := svc.DeletePlan(ctx, id); err != nil {
				return nil, err
			}
			return &QuotaPlansDeleteResult{Deleted: true, ID: id}, nil
		}),
	})
}

// adminQuotaPlansSetDefault is the `admin quota plans set-default` operation.
func adminQuotaPlansSetDefault(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_plans_set_default",
		Title:       "Set a quota plan as default",
		Summary:     "Set a quota plan as the default",
		Description: "Set a quota plan as the default for new users. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<plan-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Quota plan ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_quota_plans_set_default: plan ID is required")
			}
			if err := svc.SetDefaultPlan(ctx, id); err != nil {
				return nil, err
			}
			return &QuotaPlansSetDefaultResult{ID: id, IsDefault: true}, nil
		}),
	})
}

// allowanceLimitArg reads an optional int allowance limit (nullable).
func allowanceLimitArg(input map[string]any, key string) int {
	return derivedLimit(input, key, 0)
}

// adminQuotaAllowancesList is the `admin quota allowances list` operation.
func adminQuotaAllowancesList(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_allowances_list",
		Title:       "List quota allowances",
		Summary:     "List all quota allowances",
		Description: "List all quota allowances granted to users. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			allowances, total, err := svc.ListAllowances(ctx)
			if err != nil {
				return nil, err
			}
			return &QuotaAllowancesListResult{Count: total, Allowances: allowances}, nil
		}),
	})
}

// adminQuotaAllowancesCreate is the `admin quota allowances create` operation.
func adminQuotaAllowancesCreate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_allowances_create",
		Title:       "Create a quota allowance",
		Summary:     "Create a quota allowance for a user",
		Description: "Grant a quota allowance to a user. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "user-id", Type: catalog.ArgTypeInt, Required: true, Help: "User ID"},
			{Name: "source", Type: catalog.ArgTypeString, Help: "Allowance source reference"},
			{Name: "quota-type", Type: catalog.ArgTypeString, Help: "Allowance type (e.g. download, storage, upload, bonus)"},
			{Name: "upload-limit", Type: catalog.ArgTypeInt, Help: "Upload allowance (bytes)"},
			{Name: "download-limit", Type: catalog.ArgTypeInt, Help: "Download allowance (bytes)"},
			{Name: "storage-limit", Type: catalog.ArgTypeInt, Help: "Storage allowance (bytes)"},
			{Name: "expiry", Type: catalog.ArgTypeInt, Help: "Expiry in days from now"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			userID := catalog.IntArg(input, "user-id", 0)
			if userID == 0 {
				return nil, fmt.Errorf("admin_quota_allowances_create: user-id is required")
			}
			return svc.CreateAllowance(ctx, userID,
				catalog.StrArg(input, "source", ""),
				catalog.StrArg(input, "quota-type", ""),
				catalog.IntArg(input, "upload-limit", 0),
				catalog.IntArg(input, "download-limit", 0),
				catalog.IntArg(input, "storage-limit", 0),
				expiryTime(input),
			)
		}),
	})
}

// adminQuotaAllowancesUpdate is the `admin quota allowances update` operation.
func adminQuotaAllowancesUpdate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_allowances_update",
		Title:       "Update a quota allowance",
		Summary:     "Update a quota allowance",
		Description: "Update an existing quota allowance by grant ID. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<grant-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Allowance grant ID", PositionalOnly: true},
			{Name: "user-id", Type: catalog.ArgTypeInt, Help: "User ID"},
			{Name: "source", Type: catalog.ArgTypeString, Help: "Allowance source reference"},
			{Name: "quota-type", Type: catalog.ArgTypeString, Help: "Allowance type"},
			{Name: "upload-limit", Type: catalog.ArgTypeInt, Help: "Upload allowance (bytes)"},
			{Name: "download-limit", Type: catalog.ArgTypeInt, Help: "Download allowance (bytes)"},
			{Name: "storage-limit", Type: catalog.ArgTypeInt, Help: "Storage allowance (bytes)"},
			{Name: "expiry", Type: catalog.ArgTypeInt, Help: "Expiry in days from now"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			grantID := catalog.StrArg(input, "id", "")
			if grantID == "" {
				return nil, fmt.Errorf("admin_quota_allowances_update: grant ID is required")
			}
			return svc.UpdateAllowance(ctx, grantID,
				catalog.IntArg(input, "user-id", 0),
				catalog.StrArg(input, "source", ""),
				catalog.StrArg(input, "quota-type", ""),
				catalog.IntArg(input, "upload-limit", 0),
				catalog.IntArg(input, "download-limit", 0),
				catalog.IntArg(input, "storage-limit", 0),
				expiryTime(input),
			)
		}),
	})
}

// adminQuotaAllowancesDelete is the `admin quota allowances delete` operation.
func adminQuotaAllowancesDelete(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_allowances_delete",
		Title:       "Delete a quota allowance",
		Summary:     "Delete a quota allowance by grant ID",
		Description: "Delete a quota allowance by grant ID. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<grant-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Allowance grant ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			grantID := catalog.StrArg(input, "id", "")
			if grantID == "" {
				return nil, fmt.Errorf("admin_quota_allowances_delete: grant ID is required")
			}
			if err := svc.DeleteAllowance(ctx, grantID); err != nil {
				return nil, err
			}
			return &QuotaAllowancesDeleteResult{Deleted: true, GrantID: grantID}, nil
		}),
	})
}

// adminQuotaUserConfigsList is the `admin quota user-configs list` operation.
func adminQuotaUserConfigsList(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_user_configs_list",
		Title:       "List user quota configs",
		Summary:     "List all user quota configurations",
		Description: "List all user quota configurations. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			configs, total, err := svc.ListUserConfigs(ctx)
			if err != nil {
				return nil, err
			}
			return &QuotaUserConfigsListResult{Count: total, Configs: configs}, nil
		}),
	})
}

// adminQuotaUserConfigsUpdate is the `admin quota user-configs update`
// operation.
func adminQuotaUserConfigsUpdate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_user_configs_update",
		Title:       "Update a user quota config",
		Summary:     "Update a user's quota configuration",
		Description: "Update a user's quota configuration, such as their assigned plan or per-user limits. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "user-id", Type: catalog.ArgTypeInt, Required: true, Help: "User ID"},
			{Name: "plan-id", Type: catalog.ArgTypeInt, Help: "Quota plan ID to assign"},
			{Name: "enforcement-policy", Type: catalog.ArgTypeString, Help: "Enforcement policy (HARD_LIMITS, UNLIMITED, ALLOWANCE, THRESHOLD)"},
			{Name: "upload-limit", Type: catalog.ArgTypeInt, Help: "Upload limit override (bytes)"},
			{Name: "download-limit", Type: catalog.ArgTypeInt, Help: "Download limit override (bytes)"},
			{Name: "storage-limit", Type: catalog.ArgTypeInt, Help: "Storage limit override (bytes)"},
			{Name: "upload-threshold", Type: catalog.ArgTypeInt, Help: "Upload threshold override (bytes)"},
			{Name: "download-threshold", Type: catalog.ArgTypeInt, Help: "Download threshold override (bytes)"},
			{Name: "storage-threshold", Type: catalog.ArgTypeInt, Help: "Storage threshold override (bytes)"},
			{Name: "window-duration", Type: catalog.ArgTypeInt, Help: "Window duration override"},
			{Name: "window-start-hour", Type: catalog.ArgTypeInt, Help: "Window start hour override"},
			{Name: "window-timezone", Type: catalog.ArgTypeString, Help: "Window timezone override"},
			{Name: "window-type", Type: catalog.ArgTypeString, Help: "Window type override"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			userID := catalog.IntArg(input, "user-id", 0)
			if userID == 0 {
				return nil, fmt.Errorf("admin_quota_user_configs_update: user-id is required")
			}
			cfg := &admin.UserQuotaConfigUpdate{}
			if v := catalog.IntArgPtr(input, "plan-id"); v != nil {
				cfg.QuotaPlanId = v
			}
			if v := catalog.StrArg(input, "enforcement-policy", ""); v != "" {
				ep := admin.UserQuotaConfigUpdateEnforcementPolicy(v)
				cfg.EnforcementPolicy = &ep
			}
			setLimitFields(input, cfg)
			return svc.UpdateUserConfig(ctx, userID, cfg)
		}),
	})
}

// adminQuotaUserConfigsReset is the `admin quota user-configs reset` operation.
func adminQuotaUserConfigsReset(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_user_configs_reset",
		Title:       "Reset a user's quota plan",
		Summary:     "Reset a user's quota plan to default",
		Description: "Reset a user's assigned quota plan to the default. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<user-id>",
		Args: []catalog.OperationArg{
			{Name: "user-id", Type: catalog.ArgTypeInt, Required: true, Help: "User ID", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			userID := catalog.IntArg(input, "user-id", 0)
			if userID == 0 {
				return nil, fmt.Errorf("admin_quota_user_configs_reset: user-id is required")
			}
			if err := svc.ResetUserPlan(ctx, userID); err != nil {
				return nil, err
			}
			return &QuotaUserConfigsResetResult{UserID: userID, Reset: true}, nil
		}),
	})
}

// adminQuotaStats is the `admin quota stats` operation.
func adminQuotaStats(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_stats",
		Title:       "Quota system statistics",
		Summary:     "Show quota system statistics",
		Description: "View system-wide quota statistics. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			return svc.GetStats(ctx)
		}),
	})
}

// adminQuotaReconcile is the `admin quota reconcile` operation.
func adminQuotaReconcile(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_reconcile",
		Title:       "Reconcile quota data",
		Summary:     "Reconcile quota data",
		Description: "Reconcile quota data for all users or a specific user. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "user-id", Type: catalog.ArgTypeNullableInt, Help: "Specific user ID to reconcile (optional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			msg, count, err := svc.Reconcile(ctx, catalog.IntArgPtr(input, "user-id"))
			if err != nil {
				return nil, err
			}
			return &QuotaReconcileResult{Message: msg, UsersProcessed: count}, nil
		}),
	})
}

// adminQuotaCleanup is the `admin quota cleanup` operation.
func adminQuotaCleanup(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_quota_cleanup",
		Title:       "Cleanup expired quota data",
		Summary:     "Clean up expired quota data",
		Description: "Clean up expired quota data older than the retention period. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "retention-days", Type: catalog.ArgTypeInt, Default: "90", Help: "Retention period in days"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.quota(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			days := catalog.IntArg(input, "retention-days", 90)
			n, err := svc.Cleanup(ctx, days)
			if err != nil {
				return nil, err
			}
			return &QuotaCleanupResult{Deleted: n}, nil
		}),
	})
}

// helpers

// expiryTime converts the `expiry` days-from-now arg into a time.Time. When
// absent (0) it returns the zero time.
func expiryTime(input map[string]any) time.Time {
	days := catalog.IntArg(input, "expiry", 0)
	if days <= 0 {
		return time.Time{}
	}
	return time.Now().AddDate(0, 0, days)
}

// hasAny reports whether any of the named keys is present in input.
func hasAny(input map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := input[k]; ok {
			return true
		}
	}
	return false
}

// setLimitFields populates the limit/threshold/window override fields on a user
// config update from the nullable int args.
func setLimitFields(input map[string]any, cfg *admin.UserQuotaConfigUpdate) {
	if v := catalog.IntArgPtr(input, "upload-limit"); v != nil {
		cfg.UploadLimitBytes = v
	}
	if v := catalog.IntArgPtr(input, "download-limit"); v != nil {
		cfg.DownloadLimitBytes = v
	}
	if v := catalog.IntArgPtr(input, "storage-limit"); v != nil {
		cfg.StorageLimitBytes = v
	}
	if v := catalog.IntArgPtr(input, "upload-threshold"); v != nil {
		cfg.UploadThreshold = v
	}
	if v := catalog.IntArgPtr(input, "download-threshold"); v != nil {
		cfg.DownloadThreshold = v
	}
	if v := catalog.IntArgPtr(input, "storage-threshold"); v != nil {
		cfg.StorageThreshold = v
	}
	if v := catalog.IntArgPtr(input, "window-duration"); v != nil {
		cfg.WindowDuration = v
	}
	if v := catalog.IntArgPtr(input, "window-start-hour"); v != nil {
		cfg.WindowStartHour = v
	}
	if v := catalog.StrArg(input, "window-timezone", ""); v != "" {
		cfg.WindowTimezone = &v
	}
	if v := catalog.StrArg(input, "window-type", ""); v != "" {
		cfg.WindowType = &v
	}
}
