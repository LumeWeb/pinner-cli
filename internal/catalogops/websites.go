// Package catalogops implements websites domain operations for the operation
// catalog. Each operation drives the core websites service directly and
// returns typed data; rendering happens in the frontend wiring layer.
package catalogops

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// WebsitesDeps are the dependencies the websites operations need at
// construction time. All getters are resolved per invocation (never a
// package-init snapshot) so services always use fresh config.
type WebsitesDeps struct {
	// CfgMgr returns a live config manager for the current invocation. When
	// nil, service() passes nil to the factories (ops then fail on auth if
	// unauthenticated).
	CfgMgr func() config.Manager
	// Secure reports whether to use the secure (HTTPS) endpoint.
	Secure func() bool
	// ServiceFactory builds a websites Service. When NewAuthenticated is
	// non-nil and an auth token is available it is used; otherwise
	// ServiceFactory is used.
	ServiceFactory websites.ServiceFactoryFunc
	// NewAuthenticated builds an authenticated websites service with an
	// explicit auth token; nil means tokens are read from config via
	// ServiceFactory.
	NewAuthenticated func(cfgMgr config.Manager, secure bool, token string) (websites.Service, error)
	// GetAuthToken returns an auth token override for the current command
	// context (empty = none).
	GetAuthToken func() string
}

// config returns the live config manager for this invocation, or nil.
func (d WebsitesDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// service builds the websites Service honoring the auth-token override. The
// per-invocation --auth-token flag (threaded through input) takes precedence
// over the deps.GetAuthToken() config fallback (flag over config).
func (d WebsitesDeps) service(input map[string]any) (websites.Service, error) {
	cfgMgr := d.config()
	if cfgMgr == nil {
		return nil, fmt.Errorf("catalogops: no config manager available")
	}
	secure := false
	if d.Secure != nil {
		secure = d.Secure()
	}
	if d.NewAuthenticated != nil && d.GetAuthToken != nil {
		if t := authTokenFromInput(input); t != "" {
			return d.NewAuthenticated(cfgMgr, secure, t)
		}
		if t := d.GetAuthToken(); t != "" {
			return d.NewAuthenticated(cfgMgr, secure, t)
		}
	}
	return d.ServiceFactory(cfgMgr, secure), nil
}

// WebsitesOperations returns the catalog operations for the websites domain
// (the existing `websites` subcommand group), each driving the core
// websites.Service.
func WebsitesOperations(d WebsitesDeps) []catalog.Operation {
	return []catalog.Operation{
		websitesList(d),
		websitesGet(d),
		websitesCreate(d),
		websitesUpdate(d),
		websitesEnableIPNS(d),
		websitesDelete(d),
		websitesValidate(d),
		websitesSSLStatus(d),
		websitesConfig(d),
	}
}

// resolveWebsiteID resolves an ID-or-domain argument to a numeric website ID
// string. If the arg parses as a number it is returned as-is; otherwise the
// service lists websites and matches the domain (case-insensitively,
// DNS-normalized).
func resolveWebsiteID(ctx context.Context, svc websites.Service, arg string) (string, error) {
	if _, err := strconv.Atoi(arg); err == nil {
		return arg, nil
	}
	list, err := svc.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up website by domain: %w", err)
	}
	for _, w := range list {
		if dnsname.Equal(w.Domain, arg) {
			return strconv.Itoa(w.Id), nil
		}
	}
	return "", fmt.Errorf("website not found for domain %q", arg)
}

// resolveRequiredWebsiteID is resolveWebsiteID plus a required-argument gate
// (an empty positional is an error).
func resolveRequiredWebsiteID(ctx context.Context, svc websites.Service, input map[string]any) (string, error) {
	arg := catalog.StrArg(input, "website", "")
	if arg == "" {
		return "", fmt.Errorf("website ID or domain is required")
	}
	return resolveWebsiteID(ctx, svc, arg)
}

// websitesList is the `websites list` operation. Returns []ipfs.WebsiteItem.
func websitesList(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_list",
		Title:       "List websites",
		Summary:     "List all websites",
		Description: "List all websites for the authenticated user, returning each website's ID, domain, target CID, resolved CID, status, DNS-hosting flag and gateway.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			// Rendered as an ID/NAME/CID/... table by the CLI.
			return svc.List(ctx)
		}),
	})
}

// websitesGet is the `websites get` operation. Selects the site by <website>
// (domain or numeric ID) and returns *ipfs.WebsiteItem.
//
// The core Get may return (item, ipfs.ErrGone) when the website is in a
// broken state: the API returns 410 Gone with the website data in the body.
// When ErrGone and the item is non-nil, this handler returns the item as data
// with a nil error so the broken record is still presented. DNS-hosting
// instruction enrichment is presentation and lives in the wiring layer.
func websitesGet(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_get",
		Title:       "Get website details",
		Summary:     "Get full details of one website",
		Description: "Get full details of one website, selected by domain name or numeric ID (either works): ID, domain, CID, resolved CID, target type, status, DNS-hosting flag, validation token, gateway, and associated IPNS key / DNS zone IDs.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website ID or domain to get"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			item, err := svc.Get(ctx, id)
			if err != nil {
				if errors.Is(err, ipfs.ErrGone) && item != nil {
					return item, nil
				}
				return nil, err
			}
			// *ipfs.WebsiteItem
			return item, nil
		}),
	})
}

// websitesCreate is the `websites create` operation. Returns *ipfs.WebsiteItem.
//
// The <domain> positional and --cid are required (this handler errors if
// either is missing). --target-type defaults to "ipfs". The nullable
// --dns-hosting maps onto DnsHostingEnabled (true = managed, false =
// self-managed); omitted leaves it nil so the backend applies its default
// (managed DNS).
func websitesCreate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_create",
		Title:       "Create a website",
		Summary:     "Create a new website",
		Description: "Create a website that serves an IPFS CID under a custom domain. Takes the website field and cid (required), plus optional target-type (ipfs|ipns) and dns-hosting. Returns the created website object including its numeric ID, validation TXT token, and DNS records you must publish.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Required: true, Help: "Domain for the new website", AgentHelp: "The custom domain the website should serve under."},
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "IPFS CID to serve", AgentHelp: "The IPFS CID the website should serve."},
			{Name: "target-type", Type: catalog.ArgTypeString, Default: "ipfs", Help: "Target type (ipfs|ipns)"},
			{Name: "dns-hosting", Type: catalog.ArgTypeNullableBool, Help: "Let Pinner manage DNS for this website (true = managed, false = self-managed, omit = managed default)", AgentHelp: "true lets Pinner manage DNS; false leaves DNS self-managed. Omit to use the default (Pinner-managed DNS)."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domain := catalog.StrArg(input, "website", "")
			if domain == "" {
				return nil, fmt.Errorf("websites_create: domain is required")
			}
			cid := catalog.StrArg(input, "cid", "")
			if cid == "" {
				return nil, fmt.Errorf("websites_create: --cid is required")
			}
			targetType := catalog.StrArg(input, "target-type", "ipfs")
			req := ipfs.WebsiteRequest{
				Domain:     domain,
				TargetHash: cid,
				TargetType: targetType,
			}
			// nil (omitted) lets the backend apply its default (managed DNS);
			// true/false map onto Pinner-managed / self-managed explicitly.
			req.DnsHostingEnabled = catalog.BoolArgPtr(input, "dns-hosting")
			// *ipfs.WebsiteItem
			return svc.CreateWithOptions(ctx, req)
		}),
	})
}

// websitesUpdate is the `websites update` operation. Returns *ipfs.WebsiteItem.
//
// At least one optional field is required; this handler also enforces the
// --target-type-required-with--cid business rule. Flag mapping: rename-to ->
// req.Domain, cid -> req.TargetHash, target-type -> req.TargetType, nullable
// dns-hosting -> req.DnsHostingEnabled (nil = leave unchanged).
func websitesUpdate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_update",
		Title:       "Update a website",
		Summary:     "Update a website",
		Description: "Update an existing website: change its cid, target-type (ipfs|ipns), rename its domain via rename-to, or set dns-hosting (true = Pinner-managed, false = self-managed, omit = unchanged). Selects the site by the website field, then applies whichever optional fields are set (at least one is required). Returns the updated website object.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website ID or domain to update"},
			{Name: "rename-to", Type: catalog.ArgTypeString, Help: "New domain for the website"},
			{Name: "cid", Type: catalog.ArgTypeString, Help: "New target CID"},
			{Name: "target-type", Type: catalog.ArgTypeString, Help: "New target type (ipfs|ipns)"},
			{Name: "dns-hosting", Type: catalog.ArgTypeNullableBool, Help: "Set Pinner-managed DNS (true = managed, false = self-managed, omit = leave unchanged)", AgentHelp: "true enables Pinner-managed DNS; false disables it (self-managed). Omit to leave the current DNS hosting state unchanged."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			req := ipfs.WebsiteUpdateRequest{}
			if v := catalog.StrArg(input, "rename-to", ""); v != "" {
				req.Domain = &v
			}
			cid := catalog.StrArg(input, "cid", "")
			targetType := catalog.StrArg(input, "target-type", "")
			if cid != "" {
				req.TargetHash = &cid
			}
			if targetType != "" {
				req.TargetType = &targetType
			}
			if req.TargetHash != nil && req.TargetType == nil {
				return nil, fmt.Errorf("--target-type is required when --cid is provided")
			}
			// nil (omitted) means "leave DNS hosting unchanged"; true/false
			// toggle it on/off explicitly.
			req.DnsHostingEnabled = catalog.BoolArgPtr(input, "dns-hosting")
			// *ipfs.WebsiteItem
			return svc.UpdateWithOptions(ctx, id, req)
		}),
	})
}

// websitesEnableIPNS is the `websites enable-ipns` operation (alias 'ipns').
// Returns *ipfs.WebsiteItem.
//
// It is equivalent to `websites update <domain> --target-type ipns` (optionally
// with --cid): it auto-creates an IPNS key and publishes the current CID to it.
func websitesEnableIPNS(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_enable_ipns",
		Title:       "Enable IPNS targeting",
		Summary:     "Enable IPNS targeting for a website",
		Description: "Convert a website from IPFS to IPNS targeting (alias 'ipns'). Auto-creates an IPNS key for the site and publishes the current CID to it, or, with the cid field, publishes that CID instead. Returns the updated website including its new IPNS key ID.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website ID or domain to convert"},
			{Name: "cid", Type: catalog.ArgTypeString, Help: "Optional CID to publish to the new IPNS key"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			ipnsType := "ipns"
			req := ipfs.WebsiteUpdateRequest{TargetType: &ipnsType}
			if cid := catalog.StrArg(input, "cid", ""); cid != "" {
				req.TargetHash = &cid
			}
			// *ipfs.WebsiteItem
			return svc.UpdateWithOptions(ctx, id, req)
		}),
	})
}

// WebsiteDeleteResult is the typed data returned by the delete operation so
// the frontend can render the deleted website's identifier.
type WebsiteDeleteResult struct {
	ID string
}

// websitesDelete is the `websites delete` operation. DESTRUCTIVE. The core
// Delete returns no data; the handler returns a WebsiteDeleteResult carrying
// the resolved ID so the frontend can render a confirmation.
func websitesDelete(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_delete",
		Title:       "Delete a website",
		Summary:     "Delete a website",
		Description: "Delete a website, selected by domain name or numeric ID. DESTRUCTIVE and irreversible: there is no undo. Requires confirm=true. Does NOT delete the website's DNS zone or its IPNS keys.",
		Category:    "core",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Required: true, Help: "Website ID or domain to delete"},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the website; this is destructive and cannot be undone."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("websites_delete: confirmation is required to delete the website")
			}
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			if err := svc.Delete(ctx, id); err != nil {
				return nil, err
			}
			return &WebsiteDeleteResult{ID: id}, nil
		}),
	})
}

// websitesValidate is the `websites validate` operation. Returns
// *ipfs.WebsiteValidateResponse.
//
// The validate command's "required DNS records" hints re-fetch the website
// and config to render instructions. That enrichment is presentation and is
// left to the wiring layer, not part of the core validate data contract.
func websitesValidate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_validate",
		Title:       "Validate a website",
		Summary:     "Validate a website's DNS records",
		Description: "Validate that a website's DNS records are correctly configured (TXT validation token + _dnslink). Selects the site by domain name or numeric ID. Returns a valid/message/reason result.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website ID or domain to validate"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			// *ipfs.WebsiteValidateResponse
			return svc.Validate(ctx, id)
		}),
	})
}

// websitesSSLStatus is the `websites ssl status` operation. It takes the
// <website> positional as a DOMAIN (SSL status is keyed by domain, not ID) and
// returns *ipfs.WebsiteResponse.
//
// The SSL-status command previously wrapped this operation in a presentational
// polling loop driven by a --watch flag. That watch rendering is not part of
// the data contract and is left to the CLI wiring layer.
func websitesSSLStatus(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_ssl_status",
		Title:       "SSL certificate status",
		Summary:     "Get SSL certificate status for a website",
		Description: "Get SSL certificate status for a website domain: certificate status (active, pending, error, etc.), issuance date, last-update timestamp, and any error messages.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website domain to check SSL status for"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domain := catalog.StrArg(input, "website", "")
			if domain == "" {
				return nil, fmt.Errorf("websites_ssl_status: domain is required")
			}
			// *ipfs.WebsiteResponse
			return svc.GetSSLStatus(ctx, domain)
		}),
	})
}

// websitesConfig is the `websites config` operation. Returns
// *ipfs.WebsiteConfigResponse (gateway domain + nameservers).
func websitesConfig(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_config",
		Title:       "Website hosting configuration",
		Summary:     "Show website hosting configuration",
		Description: "Show the account-wide website hosting configuration: the Pinner gateway domain and the nameservers used for DNS hosting.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			// *ipfs.WebsiteConfigResponse
			return svc.GetConfig(ctx)
		}),
	})
}
