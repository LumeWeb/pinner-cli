package catalogops

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/portal-sdk/admin"
)

// AdminPlatformDomainsListResult is the typed data returned by the platform
// domains list operation; the core service returns ([]*admin.PlatformDomain,
// total, error) which a catalog Handler cannot express, so they are bundled.
type AdminPlatformDomainsListResult struct {
	Count           int                    `json:"count"`
	PlatformDomains []*admin.PlatformDomain `json:"platform_domains"`
}

// adminPlatformDomainsList is the `admin platform-domains list` operation.
func adminPlatformDomainsList(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_platform_domains_list",
		Title:       "List platform domains",
		Summary:     "List registered platform domains",
		Description: "List all registered platform-owned root domains that users can claim free subdomains under, including disabled ones. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.platformDomains(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domains, total, err := svc.ListPlatformDomains(ctx)
			if err != nil {
				return nil, err
			}
			return &AdminPlatformDomainsListResult{Count: total, PlatformDomains: domains}, nil
		}),
	})
}

// adminPlatformDomainsRegister is the `admin platform-domains register`
// operation.
func adminPlatformDomainsRegister(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_platform_domains_register",
		Title:       "Register a platform domain",
		Summary:     "Register a platform-owned root domain",
		Description: "Register a platform-owned root domain that users can claim free subdomains under, e.g. ipfs.pin.xyz. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Platform root domain, e.g. ipfs.pin.xyz"},
			{Name: "namespace", Type: catalog.ArgTypeString, Required: true, Help: "Domain namespace: icann, hns, etc."},
			// Nullable so an omitted flag leaves Enabled nil (backend default)
			// rather than forcing false; an explicit --enabled=false disables.
			{Name: "enabled", Type: catalog.ArgTypeNullableBool, Required: false, Help: "Enable the platform domain so users can claim subdomains under it"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.platformDomains(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domain := catalog.StrArg(input, "domain", "")
			if domain == "" {
				return nil, fmt.Errorf("admin_platform_domains_register: domain is required")
			}
			namespace := catalog.StrArg(input, "namespace", "")
			if namespace == "" {
				return nil, fmt.Errorf("admin_platform_domains_register: namespace is required")
			}
			req := &admin.PlatformDomainRequest{Domain: domain, Namespace: namespace}
			if enabled := catalog.BoolArgPtr(input, "enabled"); enabled != nil {
				req.Enabled = enabled
			}
			return svc.RegisterPlatformDomain(ctx, req)
		}),
	})
}

// adminPlatformDomainsUpdate is the `admin platform-domains update` operation.
func adminPlatformDomainsUpdate(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_platform_domains_update",
		Title:       "Update a platform domain",
		Summary:     "Enable or disable a platform domain",
		Description: "Enable or disable a registered platform root. Disabling prevents new claims but does not delete existing bindings. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Platform domain ID", PositionalOnly: true},
			{Name: "enabled", Type: catalog.ArgTypeBool, Required: true, Help: "Enable (true) or disable (false) the platform domain"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.platformDomains(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_platform_domains_update: platform domain ID is required")
			}
			req := &admin.PlatformDomainUpdateRequest{Enabled: catalog.BoolArg(input, "enabled", false)}
			return svc.UpdatePlatformDomain(ctx, id, req)
		}),
	})
}

// AdminPlatformDomainsDeleteResult is the typed data returned by the delete
// operation.
type AdminPlatformDomainsDeleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// adminPlatformDomainsDelete is the `admin platform-domains delete` operation.
// DESTRUCTIVE: requires confirm=true.
func adminPlatformDomainsDelete(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_platform_domains_delete",
		Title:       "Delete a platform domain",
		Summary:     "Delete a registered platform domain",
		Description: "Remove a registered platform root. Existing subdomain bindings remain but can no longer be reconciled as platform subdomains. DESTRUCTIVE and irreversible: requires confirm=true. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Platform domain ID", PositionalOnly: true},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the platform domain; this is destructive and cannot be undone."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("admin_platform_domains_delete: confirmation is required to delete the platform domain")
			}
			svc, err := d.platformDomains(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_platform_domains_delete: platform domain ID is required")
			}
			if err := svc.DeletePlatformDomain(ctx, id); err != nil {
				return nil, err
			}
			return &AdminPlatformDomainsDeleteResult{Deleted: true, ID: id}, nil
		}),
	})
}

// adminPlatformDomainsBind is the `admin platform-domains bind` operation.
func adminPlatformDomainsBind(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_platform_domains_bind",
		Title:       "Bind a website to a platform domain",
		Summary:     "Bind an operator-owned website to a platform domain",
		Description: "Bind an operator-owned website directly to the root apex of a platform domain (e.g. pinner.site). The platform root's DNS zone is auto-created on first use. Requires admin privileges.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Platform domain ID", PositionalOnly: true},
			{Name: "website-id", Type: catalog.ArgTypeInt, Required: true, Help: "ID of the operator-owned website to bind"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.platformDomains(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_platform_domains_bind: platform domain ID is required")
			}
			websiteID := catalog.IntArg(input, "website-id", 0)
			if websiteID == 0 {
				return nil, fmt.Errorf("admin_platform_domains_bind: website-id is required")
			}
			req := &admin.PlatformDomainBindRequest{WebsiteId: websiteID}
			return svc.BindWebsiteToPlatformDomain(ctx, id, req)
		}),
	})
}
