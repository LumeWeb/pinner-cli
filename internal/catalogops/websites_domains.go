// Package catalogops implements websites domain operations for the operation
// catalog. Each operation drives the core websites service directly and
// returns typed data; rendering happens in the frontend wiring layer.
package catalogops

import (
	"context"
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// boolTrue returns a pointer to true.
func boolTrue() *bool {
	b := true
	return &b
}

// boolFalse returns a pointer to false.
func boolFalse() *bool {
	b := false
	return &b
}

// websitesDomainsList is the `websites domains list` operation. Returns
// []ipfs.DomainResponse.
func websitesDomainsList(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_list",
		Title:       "List domains of a website",
		Summary:     "List all domains bound to a website",
		Description: "List every domain binding on a website, selected by domain name or numeric ID: each binding's ID, domain, namespace, status, DNS-hosting flag, zone name and delegation. Requires exactly one website selector.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<website>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Required: true, Help: "Website ID or domain to list domains for"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			websiteID, err := resolveRequiredWebsiteID(ctx, svc, input)
			if err != nil {
				return nil, err
			}
			// []ipfs.DomainResponse
			return svc.ListDomains(ctx, websiteID)
		}),
	})
}

// websitesDomainsAdd is the `websites domains add` operation. Returns
// *ipfs.DomainResponse.
//
// The website may be given explicitly (ID or domain) or auto-selected when
// exactly one website exists. Auto-selection mirrors the legacy CLI
// resolveAddTarget behaviour.
func websitesDomainsAdd(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_add",
		Title:       "Bind a domain to a website",
		Summary:     "Add a domain to a website",
		Description: "Bind a domain to a website under a namespace (icann or hns, default icann). The website may be given as an ID or domain, or auto-selected when the account has exactly one website. Returns the bound domain object including its numeric binding ID, status and DNS-hosting flag.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "[<website>] <domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Required: false, Help: "Website ID or domain to bind the domain to; auto-selected if exactly one website", AgentHelp: "The website (ID or domain) to bind the domain to. Omit to auto-select when the account has exactly one website."},
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain to bind"},
			{Name: "namespace", Type: catalog.ArgTypeString, Default: "icann", Sources: []string{"PINNER_DOMAIN_NAMESPACE"}, Help: "Domain namespace: icann or hns"},
			{Name: "platform-domain", Type: catalog.ArgTypeString, Required: false, Help: "Platform (free-subdomain) root domain to claim a subdomain under, e.g. pinned.site"},
			{Name: "platform-namespace", Type: catalog.ArgTypeString, Required: false, Help: "Namespace within the platform domain to claim under"},
			{Name: "generate", Type: catalog.ArgTypeBool, Default: "false", Required: false, Help: "Ask the platform to auto-generate a subdomain label instead of supplying one"},
			{Name: "label", Type: catalog.ArgTypeString, Required: false, Help: "Explicit subdomain label to claim under a platform domain"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domain := catalog.StrArg(input, "domain", "")
			if domain == "" {
				return nil, fmt.Errorf("websites_domains_add: domain is required")
			}

			websiteArg := catalog.StrArg(input, "website", "")
			var websiteID string
			if websiteArg == "" {
				// Auto-select when there is exactly one website.
				list, err := svc.List(ctx)
				if err != nil {
					return nil, fmt.Errorf("websites_domains_add: failed to list websites: %w", err)
				}
				switch len(list) {
				case 0:
					return nil, fmt.Errorf("websites_domains_add: no websites found; create a website first")
				case 1:
					websiteID = fmt.Sprintf("%d", list[0].Id)
				default:
					return nil, fmt.Errorf("websites_domains_add: multiple websites found (%d); specify which website to add the domain to", len(list))
				}
			} else {
				id, err := websites.ResolveWebsiteID(ctx, svc, websiteArg)
				if err != nil {
					return nil, err
				}
				websiteID = id
			}

			namespace := catalog.StrArg(input, "namespace", "icann")
			if namespace != "icann" && namespace != "hns" {
				return nil, fmt.Errorf("websites_domains_add: invalid namespace %q: must be 'icann' or 'hns'", namespace)
			}
			req := ipfs.DomainRequest{
				Domain:    domain,
				Namespace: namespace,
			}
			// Platform (free-subdomain) claiming: when a platform-domain root is
			// supplied, pass the optional platform fields through so the portal
			// can mint a subdomain at bind time (label provided explicitly, or
			// auto-generated via generate).
			if pd := catalog.StrArg(input, "platform-domain", ""); pd != "" {
				req.PlatformDomain = &pd
			}
			if pns := catalog.StrArg(input, "platform-namespace", ""); pns != "" {
				req.PlatformNamespace = &pns
			}
			if catalog.BoolArg(input, "generate", false) {
				g := true
				req.Generate = &g
			}
			if label := catalog.StrArg(input, "label", ""); label != "" {
				req.Label = &label
			}
			// *ipfs.DomainResponse
			return svc.BindDomain(ctx, websiteID, req)
		}),
	})
}

// WebsiteDomainsRemoveResult is the typed data returned by the domain remove
// operation so the frontend can render the unbound binding and its ID.
type WebsiteDomainsRemoveResult struct {
	Deleted  bool   `json:"deleted"`
	DomainID string `json:"domain_id"`
}

// resolvePair collapses an enable/disable flag pair (each nullable-bool) into a
// single tri-state outcome: whether to enable, whether to disable, and whether
// the two forms were both supplied (a conflict). An explicit false on an enable
// form is treated as its disable counterpart (the user is asking to turn the
// setting off), so `--flag=false` is never a silent no-op.
func resolvePair(enable, disable *bool) (on bool, off bool, conflict bool) {
	if enable != nil && disable != nil {
		return false, false, true
	}
	if enable != nil {
		if *enable {
			return true, false, false
		}
		return false, true, false
	}
	if disable != nil {
		if *disable {
			return false, true, false
		}
		return true, false, false
	}
	return false, false, false
}

// websitesDomainsRemove is the `websites domains rm` operation. The core
// UnbindDomain returns no data; the handler returns a
// WebsiteDomainsRemoveResult carrying the deleted binding ID.
func websitesDomainsRemove(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_remove",
		Title:       "Remove a domain binding",
		Summary:     "Unbind a domain from its website",
		Description: "Remove a domain binding from its website. The domain argument can be the domain name or its numeric binding ID; the owning website is resolved automatically. Returns a deleted/domain_id result confirming the removal.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or binding ID to remove"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domainArg := catalog.StrArg(input, "domain", "")
			if domainArg == "" {
				return nil, fmt.Errorf("websites_domains_remove: domain is required")
			}
			websiteID, domainID, err := websites.ResolveDomainBinding(ctx, svc, domainArg)
			if err != nil {
				return nil, err
			}
			if err := svc.UnbindDomain(ctx, websiteID, domainID); err != nil {
				return nil, err
			}
			return &WebsiteDomainsRemoveResult{Deleted: true, DomainID: domainID}, nil
		}),
	})
}

// websitesDomainsVerify is the `websites domains verify` operation. Returns
// *ipfs.DomainResponse.
func websitesDomainsVerify(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_verify",
		Title:       "Verify a domain binding",
		Summary:     "Verify a domain's DNS delegation",
		Description: "Verify that a bound domain's DNS delegation is correctly configured. The domain argument can be the domain name or its numeric binding ID; the owning website is resolved automatically. Returns the domain's status and delegation after verification.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or binding ID to verify"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domainArg := catalog.StrArg(input, "domain", "")
			if domainArg == "" {
				return nil, fmt.Errorf("websites_domains_verify: domain is required")
			}
			websiteID, domainID, err := websites.ResolveDomainBinding(ctx, svc, domainArg)
			if err != nil {
				return nil, err
			}
			// *ipfs.DomainResponse
			return svc.VerifyDomain(ctx, websiteID, domainID)
		}),
	})
}

// websitesDomainsDNSRequirements is the `websites domains dns-requirements`
// operation. Returns *ipfs.DomainResponse.
func websitesDomainsDNSRequirements(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_dns_requirements",
		Title:       "DNS requirements for a domain",
		Summary:     "Show DNS records needed to complete domain delegation",
		Description: "Show the DNS records a user must publish to complete delegation for a bound domain. For HNS namespaces this is the delegation bundle (parent NS/GLUE/DS and authoritative NS/TLSA). The domain argument can be the domain name or its numeric binding ID; the owning website is resolved automatically.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or binding ID to get DNS requirements for"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domainArg := catalog.StrArg(input, "domain", "")
			if domainArg == "" {
				return nil, fmt.Errorf("websites_domains_dns_requirements: domain is required")
			}
			websiteID, domainID, err := websites.ResolveDomainBinding(ctx, svc, domainArg)
			if err != nil {
				return nil, err
			}
			// *ipfs.DomainResponse
			return svc.GetDomainDNSRequirements(ctx, websiteID, domainID)
		}),
	})
}

// websitesDomainsDANERepublish is the `websites domains dane republish`
// operation. Returns *ipfs.DomainDANERepublishResponse.
func websitesDomainsDANERepublish(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_dane_republish",
		Title:       "Republish DANE records",
		Summary:     "Force re-publication of a domain's DANE TLSA record",
		Description: "Force re-publication of a bound domain's DANE records (the _443._tcp TLSA RRset) into the managed authoritative zone, to recover a TLSA that was deleted or missing and not re-published by certificate renewal. The domain argument can be the domain name or its numeric binding ID; the owning website is resolved automatically. Returns the republished record's status and TLSA value.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or binding ID to republish DANE for"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domainArg := catalog.StrArg(input, "domain", "")
			if domainArg == "" {
				return nil, fmt.Errorf("websites_domains_dane_republish: domain is required")
			}
			websiteID, domainID, err := websites.ResolveDomainBinding(ctx, svc, domainArg)
			if err != nil {
				return nil, err
			}
			// *ipfs.DomainDANERepublishResponse
			return svc.RepublishDANE(ctx, websiteID, domainID)
		}),
	})
}

// websitesDomainsUpdate is the `websites domains update` operation. Returns
// *ipfs.DomainResponse.
//
// Each field has a positive and negative control flag; when neither is set the
// field is left nil on the request so the server leaves it unchanged.
func websitesDomainsUpdate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_domains_update",
		Title:       "Update a domain binding",
		Summary:     "Update DNS hosting / primary flags on a domain binding",
		Description: "Update a bound domain's per-domain DNS control: enable or disable portal-managed DNS hosting (dns_hosting / no_dns_hosting) and promote or demote the binding as primary (primary / no_primary). Only the flags set are sent; unset fields are left unchanged. The domain argument can be the domain name or its numeric binding ID; the owning website is resolved automatically. Returns the updated domain object.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or binding ID to update"},
			{Name: "dns-hosting", Type: catalog.ArgTypeNullableBool, Help: "Enable portal-managed DNS hosting for this binding"},
			{Name: "no-dns-hosting", Type: catalog.ArgTypeNullableBool, Help: "Disable portal-managed DNS hosting for this binding"},
			{Name: "primary", Type: catalog.ArgTypeNullableBool, Help: "Promote this binding to primary"},
			{Name: "no-primary", Type: catalog.ArgTypeNullableBool, Help: "Demote this binding from primary"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domainArg := catalog.StrArg(input, "domain", "")
			if domainArg == "" {
				return nil, fmt.Errorf("websites_domains_update: domain is required")
			}

			// The four platform flags are nullable-bool so an explicit
			// `--flag=false` is distinguishable from an omitted flag. An
			// explicit false on an enable form (e.g. --dns-hosting=false) means
			// the user wants to turn that setting off, so it maps to the
			// corresponding disable — no silent no-op.
			dnsHosting := catalog.BoolArgPtr(input, "dns-hosting")
			noDNSHosting := catalog.BoolArgPtr(input, "no-dns-hosting")
			primary := catalog.BoolArgPtr(input, "primary")
			noPrimary := catalog.BoolArgPtr(input, "no-primary")

			// Resolve to two tri-state outcomes: enable(P) / disable(N) each a
			// (*bool, present). Both forms present is a conflict.
			enableDNS, disableDNS, dnsConflict := resolvePair(dnsHosting, noDNSHosting)
			setPrimary, unsetPrimary, primaryConflict := resolvePair(primary, noPrimary)
			if dnsConflict {
				return nil, fmt.Errorf("websites_domains_update: dns_hosting and no_dns_hosting cannot both be set")
			}
			if primaryConflict {
				return nil, fmt.Errorf("websites_domains_update: primary and no_primary cannot both be set")
			}
			if !enableDNS && !disableDNS && !setPrimary && !unsetPrimary {
				return nil, fmt.Errorf("websites_domains_update: at least one of dns_hosting, no_dns_hosting, primary or no_primary is required")
			}

			req := ipfs.DomainUpdateRequest{}
			if enableDNS {
				req.DnsHostingEnabled = boolTrue()
			} else if disableDNS {
				req.DnsHostingEnabled = boolFalse()
			}
			if setPrimary {
				req.Primary = boolTrue()
			} else if unsetPrimary {
				req.Primary = boolFalse()
			}

			websiteID, domainID, err := websites.ResolveDomainBinding(ctx, svc, domainArg)
			if err != nil {
				return nil, err
			}
			// *ipfs.DomainResponse
			return svc.UpdateDomain(ctx, websiteID, domainID, req)
		}),
	})
}
