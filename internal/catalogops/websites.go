// Package catalogops implements websites domain operations for the operation
// catalog. Each operation drives the core websites service directly and
// returns typed data; rendering happens in the frontend wiring layer.
package catalogops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"

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
		websitesDomainsList(d),
		websitesDomainsAdd(d),
		websitesDomainsRemove(d),
		websitesDomainsVerify(d),
		websitesDomainsDNSRequirements(d),
		websitesDomainsDANERepublish(d),
		websitesDomainsUpdate(d),
		websitesPlatformDomainsList(d),
		websitesPlatformDomainAvailability(d),
	}
}

// resolveRequiredWebsiteID reads the <website> positional (ID or domain) and
// resolves it to a numeric website ID via the core resolver, erroring when the
// argument is empty.
func resolveRequiredWebsiteID(ctx context.Context, svc websites.Service, input map[string]any) (string, error) {
	arg := catalog.StrArg(input, "website", "")
	if arg == "" {
		return "", fmt.Errorf("website ID or domain is required")
	}
	return websites.ResolveWebsiteID(ctx, svc, arg)
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
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{
				Name: "domain", Type: catalog.ArgTypeString,
				Help: "Filter websites whose domain contains this value",
			},
			catalog.OperationArg{
				Name: "status", Type: catalog.ArgTypeString,
				Help: "Filter websites by status",
			},
			catalog.OperationArg{
				Name: "target-type", Type: catalog.ArgTypeString,
				Help: "Filter websites by target type (ipfs or ipns)",
			},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}

			page := catalog.ParseList(input)
			opts := websites.ListOptions{
				Start:      page.Start,
				Limit:      page.Limit,
				Domain:     catalog.StrArg(input, "domain", ""),
				Status:     catalog.StrArg(input, "status", ""),
				TargetType: catalog.StrArg(input, "target-type", ""),
			}
			sites, err := svc.List(ctx, opts)
			if err != nil {
				return nil, err
			}

			headers := []string{"ID", "NAME", "CID", "RESOLVED CID", "STATUS", "DNS", "SUBDOMAIN", "GATEWAY", "VALIDATION", "CREATED"}
			rows := make([][]string, 0, len(sites))
			for _, w := range sites {
				validation := ""
				if w.Status == "active" {
					validation = "validated"
				} else if w.Expired {
					validation = "expired"
				} else if w.ValidationToken != "" {
					validation = websites.StripValidationPrefix(w.ValidationToken)
				}
				gateway := ""
				if w.GatewayDomain != nil {
					gateway = *w.GatewayDomain
				}
				resolvedCID := "-"
				if w.ActiveCid != nil {
					resolvedCID = *w.ActiveCid
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", w.Id), w.Domain, w.TargetHash, resolvedCID, w.Status,
					fmt.Sprintf("%t", w.DnsHostingEnabled), fmt.Sprintf("%t", w.IsSubdomain),
					gateway, validation, w.Created.Format("2006-01-02 15:04:05"),
				})
			}
			return NewListResult(sites, ListResultMeta{
				Noun:    "website(s)",
				Headers: headers,
				Rows:    rows,
			}), nil
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
// A website needs a destination: either a user-owned custom domain (the
// positional) or a platform-provided (free) subdomain claimed at create time.
// --cid is always required. The destination type is resolved, not switched:
// --platform (or any claim field) forces a platform claim; otherwise a supplied
// domain is parsed — if it is a subdomain of an enabled platform root it is a
// platform claim (label/root derived by parsing), otherwise it is a custom
// domain. With no domain at all the call defaults to a minted platform
// subdomain (generate). --target-type defaults to "ipfs". The agent-only args
// (platform, platform-domain, platform-namespace, generate, label) are omitted
// from the CLI surface, which derives platform claims by parsing instead.
func websitesCreate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_create",
		Title:       "Create a website",
		Summary:     "Create a new website",
		Description: "Create a website that serves an IPFS CID. With only --cid, a platform (free) subdomain is minted automatically. Provide a custom domain as the positional for a user-owned domain; a subdomain of a platform root (e.g. myapp.pinned.site) is auto-detected and claimed as a platform subdomain. Returns the created website with validation token and DNS records.",
		AgentDescription: "Create a website that serves an IPFS CID. Provide cid plus a destination: EITHER a custom domain (positional) OR a platform-provided (free) subdomain claim. The type is derived automatically — with no domain and no claim fields it defaults to a minted platform subdomain; with a domain that is a subdomain of an enabled platform root (e.g. myapp.pinned.site) it claims that subdomain by parsing; with any other domain it is a custom domain. Set platform=true to force a platform claim, pairing label or generate (and optionally platform-domain / platform-namespace). For a custom domain: call websites_create with {\"cid\": \"<cid>\", \"website\": \"<domain>\", \"target-type\": \"ipfs\"} (target-type and dns-hosting are optional). If the user has NO domain, do not invent a custom domain — create with only {\"cid\": \"<cid>\"} (auto-mint) or pass {\"cid\": \"<cid>\", \"platform\": true, \"generate\": true}. Do not infer a desire for custom naming from a generic request to create or publish a website; default to no domain (auto-generated subdomain) unless the user explicitly supplies or requests a specific label or domain. For newly uploaded content, use the CID returned directly by an upload tool — do NOT call pins_add after upload. pins_add is only needed when the CID originated outside Pinner and must be imported from IPFS. Returns the created website (numeric ID, validation TXT token, DNS records to publish).",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Required: false, Help: "Custom domain for the new website (optional: a platform subdomain is minted when omitted)", AgentHelp: "The destination domain. A subdomain of a platform root (e.g. myapp.pinned.site) is treated as a platform claim (label/root parsed); any other domain is a custom domain. Omit to default to a minted platform subdomain."},
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "IPFS CID to serve", AgentHelp: "The IPFS CID to serve. If from a Pinner upload tool, use its returned CID directly (already pinned, no pins_add). Only call pins_add first when the CID is external to Pinner."},
			{Name: "target-type", Type: catalog.ArgTypeString, Default: "ipfs", Help: "Target type (ipfs|ipns)"},
			{Name: "dns-hosting", Type: catalog.ArgTypeNullableBool, Help: "Let Pinner manage DNS for this website (true = managed, false = self-managed, omit = managed default; ignored for platform subdomains, which are always managed)", AgentHelp: "true lets Pinner manage DNS; false leaves DNS self-managed. Omit to use the default. Ignored for platform subdomains (always managed)."},
			{Name: "platform", Type: catalog.ArgTypeBool, Required: false, AgentOnly: true, Help: "Claim a platform (free) subdomain", AgentHelp: "Set true to force a platform (free) subdomain claim. Pair with label or generate; omit the domain positional. The type is otherwise derived automatically."},
			{Name: "platform-domain", Type: catalog.ArgTypeString, Required: false, AgentOnly: true, Help: "Platform root to claim under (default: platform default)", AgentHelp: "The platform root to claim a free subdomain under (e.g. pinned.site). Optional; when set, restricts the claim to this root. Use with platform plus label or generate."},
			{Name: "platform-namespace", Type: catalog.ArgTypeString, Required: false, AgentOnly: true, Help: "Namespace within the platform domain to claim under (default icann)"},
			{Name: "generate", Type: catalog.ArgTypeBool, Required: false, AgentOnly: true, Help: "Auto-generate a subdomain label (mutually exclusive with label)", AgentHelp: "Set true to let the platform auto-generate the subdomain label. Mutually exclusive with label."},
			{Name: "label", Type: catalog.ArgTypeString, Required: false, AgentOnly: true, Help: "Explicit subdomain label to claim (mutually exclusive with generate)", AgentHelp: "Explicit subdomain label to claim under a platform domain. Mutually exclusive with generate."},
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
			cid := catalog.StrArg(input, "cid", "")
			if cid == "" {
				return nil, fmt.Errorf("websites_create: --cid is required")
			}
			targetType := catalog.StrArg(input, "target-type", "ipfs")

			platformFlag := catalog.BoolArg(input, "platform", false)
			pd := catalog.StrArg(input, "platform-domain", "")
			pns := catalog.StrArg(input, "platform-namespace", "")
			generate := catalog.BoolArg(input, "generate", false)
			label := catalog.StrArg(input, "label", "")

			// Resolve the destination type. A supplied domain may parse as a
			// platform subdomain (label.root); detect it against the enabled
			// platform roots. If roots can't be fetched, degrade to treating the
			// domain as a custom domain rather than failing the create.
			domainIsPlatform := false
			if domain != "" {
				if roots, rerr := svc.ListPlatformDomains(ctx); rerr == nil && roots != nil {
					if rpd, rns, rlbl, ok := platformClaimForDomain(domain, roots); ok {
						domainIsPlatform = true
						if pd == "" {
							pd = rpd
						}
						if pns == "" {
							pns = rns
						}
						if label == "" && !generate {
							label = rlbl
						}
					}
				}
			}

			explicitPlatform := platformFlag || pd != "" || generate || label != ""
			isPlatform := domainIsPlatform || explicitPlatform

			req := ipfs.WebsiteRequest{
				TargetHash: cid,
				TargetType: targetType,
			}
			if isPlatform || (domain == "" && !explicitPlatform) {
				// Platform path, including the default mint (no domain, no
				// explicit intent). Sending a custom-looking domain together
				// with platform intent is ambiguous — reject it so the user's
				// --dns-hosting choice isn't silently dropped.
				if domain != "" && !domainIsPlatform {
					return nil, fmt.Errorf("websites_create: supply a custom domain (positional) OR claim a platform subdomain, not both")
				}
				if label == "" && domain == "" {
					generate = true // default: mint a platform subdomain
				}
				if generate && label != "" {
					return nil, fmt.Errorf("websites_create: --generate and --label are mutually exclusive; provide one to claim a platform subdomain")
				}
				// Platform subdomains are always DNS-managed by the platform.
				managed := true
				req.DnsHostingEnabled = &managed
				if pd != "" {
					req.PlatformDomain = &pd
				}
				if pns != "" {
					req.PlatformNamespace = &pns
				}
				if generate {
					req.Generate = &generate
				}
				if label != "" {
					req.Label = &label
				}
			} else {
				req.Domain = &domain
				// nil (omitted) lets the backend apply its default (managed DNS);
				// true/false map onto Pinner-managed / self-managed explicitly.
				req.DnsHostingEnabled = catalog.BoolArgPtr(input, "dns-hosting")
			}
			// Translate backend reason codes (e.g. CID_NOT_PINNED,
			// IPNS_KEY_NOT_FOUND, DNS_VALIDATION_FAILED, subdomain-claim errors)
			// into clear, actionable messages; this same handler drives both the
			// CLI and the MCP tool-call surface. *ipfs.WebsiteItem
			result, err := svc.CreateWithOptions(ctx, req)
			return result, websites.TranslateErrorWithCID(err, cid)
		}),
	})
}

// platformClaimForDomain resolves a supplied domain to a platform (free)
// subdomain claim when the domain is a subdomain of an enabled platform root
// (e.g. "myapp.pinned.site" under root "pinned.site"). It returns the matched
// platform domain, its namespace, and the claim label; ok is false when the
// domain is not a recognized platform subdomain. The longest matching root
// wins so a deeper root (e.g. "app.sub.root") beats its parent.
func platformClaimForDomain(domain string, roots *ipfs.PlatformDomainListResponse) (platformDomain, namespace, label string, ok bool) {
	bestPD, bestNS, bestLabel := "", "", ""
	bestLen := 0
	for _, r := range roots.Data {
		if !r.Enabled {
			continue
		}
		lbl := subdomainLabel(domain, r.Domain)
		if lbl == "" {
			continue // not a subdomain of this root (or the bare root itself)
		}
		if len(r.Domain) > bestLen {
			bestLen = len(r.Domain)
			bestPD, bestNS, bestLabel = r.Domain, r.Namespace, lbl
		}
	}
	if bestPD == "" {
		return "", "", "", false
	}
	return bestPD, bestNS, bestLabel, true
}

// subdomainLabel returns the case-normalized (lowercased) label when domain is
// a subdomain of root, or "" when it is not (or when domain is the bare root
// itself). DNS labels are case-insensitive, so the comparison is lowercased:
// "MyApp.pinned.site" against root "pinned.site" yields label "myapp" instead
// of falling through to the custom-domain branch.
func subdomainLabel(domain, root string) string {
	dl := strings.ToLower(domain)
	lbl := strings.TrimSuffix(dl, "."+strings.ToLower(root))
	if lbl == "" || lbl == dl {
		return ""
	}
	return lbl
}

// websitesUpdate is the `websites update` operation. Returns *ipfs.WebsiteItem.
//
// At least one optional field is required. Flag mapping: rename-to ->
// req.Domain, cid -> req.TargetHash, target-type -> req.TargetType, nullable
// dns-hosting -> req.DnsHostingEnabled (nil = leave unchanged).
//
// When cid is set without target-type, the site's current target type is
// preserved (fetched via Get) so a bare cid update is unambiguous and cannot
// accidentally flip IPFS<->IPNS targeting.
func websitesUpdate(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_update",
		Title:       "Update a website",
		Summary:     "Update a website",
		Description: "Update an existing website: change its cid, target-type (ipfs|ipns), rename its domain (rename-to), or set dns-hosting (true = Pinner-managed, false = self-managed, omit = unchanged). Select the site by website; set at least one optional field. With only cid set (no target-type), the site's current target type is preserved automatically.",
		AgentDescription: "Update an existing website: change its cid, target-type (ipfs|ipns), rename its domain (rename-to), or set dns-hosting (true = Pinner-managed, false = self-managed, omit = unchanged). Select the site by website; set at least one optional field. A CID produced by an upload tool is already pinned and usable directly. Only when the CID is an EXTERNAL IPFS CID must you call pins_add first; a bare update with an unpinned CID fails with CID_NOT_PINNED. With only cid set (no target-type), the site's current target type is preserved automatically. For the full guided flow, fetch the website-update prompt (prompts/get website-update).",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "website", Type: catalog.ArgTypeString, Help: "Website ID or domain to update"},
			{Name: "rename-to", Type: catalog.ArgTypeString, Help: "New domain for the website"},
			{Name: "cid", Type: catalog.ArgTypeString, Help: "New target CID", AgentHelp: "The IPFS CID to serve. If produced by a Pinner upload tool, it is already pinned — use it directly. Only call pins_add(cids=[\"<cid>\"], wait=true) first when the CID is an external IPFS CID; an unpinned CID fails with CID_NOT_PINNED. With a bare cid (no target-type), the site's current targeting is preserved automatically."},
			{Name: "target-type", Type: catalog.ArgTypeString, Help: "New target type (ipfs|ipns); when omitted with cid, the site's current target type is preserved"},
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
				// A bare cid is ambiguous (an IPFS CID vs an IPNS name). Preserve
				// the site's current target type so agents can update the CID
				// without guessing and without accidentally flipping IPFS<->IPNS.
				current, curErr := svc.Get(ctx, id)
				if curErr != nil {
					return nil, fmt.Errorf("resolve current target type for %s: %w", id, curErr)
				}
				tt := current.TargetType
				if tt == "" {
					tt = "ipfs"
				}
				req.TargetType = &tt
			}
			// nil (omitted) means "leave DNS hosting unchanged"; true/false
			// toggle it on/off explicitly.
			req.DnsHostingEnabled = catalog.BoolArgPtr(input, "dns-hosting")
			// *ipfs.WebsiteItem
			result, err := svc.UpdateWithOptions(ctx, id, req)
			return result, websites.TranslateErrorWithCID(err, cid)
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
			cid := catalog.StrArg(input, "cid", "")
			if cid != "" {
				req.TargetHash = &cid
			}
			// *ipfs.WebsiteItem
			result, err := svc.UpdateWithOptions(ctx, id, req)
			return result, websites.TranslateErrorWithCID(err, cid)
		}),
	})
}

// WebsiteDeleteResult is the typed data returned by the delete operation so
// the frontend can render the deleted website's identifier.
type WebsiteDeleteResult struct {
	ID string `json:"id"`
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
		AgentDescription: "Validates that a website's DNS records are correctly configured (TXT validation token + _dnslink). Call this after websites_create to confirm DNS propagation. For managed-DNS platform subdomains, validation typically passes within 30-60s of creation; if it fails, wait and retry rather than treating it as a creation failure. For self-managed DNS, ensure the _dnslink TXT and validation TXT are published before calling.",
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
			result, err := svc.Validate(ctx, id)
			return result, websites.TranslateError(err)
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

// websitesPlatformDomainsList is the `websites platform-domains` operation /
// `websites_platform_domains_list` MCP tool. Lists the platform (free-subdomain)
// root domains that are enabled and available for users to claim subdomains
// under. Returns *ipfs.PlatformDomainListResponse (data plus total).
func websitesPlatformDomainsList(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_platform_domains_list",
		Title:       "List platform domains",
		Summary:     "List available platform subdomain roots",
		Description: "List the platform-owned root domains that are enabled and available for users to claim free subdomains under. Each entry carries the root domain, its DNS namespace, zone id, and whether it is enabled.",
		AgentDescription: "List the platform-owned root domains that are enabled and available for users to claim free subdomains under. Only needed when the user explicitly requests a specific subdomain label — use this to discover roots before checking availability with websites_platform_domain_availability. If the user has no label preference, skip this and call websites_create with no domain (auto-generates a platform subdomain).",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			// *ipfs.PlatformDomainListResponse
			return svc.ListPlatformDomains(ctx)
		}),
	})
}

// websitesPlatformDomainAvailability is the `websites platform-domain
// availability` operation / `websites_platform_domain_availability` MCP tool.
// Checks whether a candidate subdomain label is claimable on each enabled
// platform (free-subdomain) root. label is required.
// Returns *ipfs.PlatformAvailabilityResponse (label plus one
// PlatformAvailabilityResult per root).
func websitesPlatformDomainAvailability(d WebsitesDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "websites_platform_domain_availability",
		Title:       "Check platform domain availability",
		Summary:     "Check if a label is available as a platform subdomain",
		Description: "Check whether a candidate subdomain label is claimable on each enabled platform (free-subdomain) root. label is required. Returns one availability result per platform-owned root.",
		AgentDescription: "Check whether a candidate subdomain label is claimable on each enabled platform (free-subdomain) root. label is required. Returns one availability result per platform-owned root. Use only when a concrete subdomain label has already been supplied by the user or is required by an explicit user request for custom naming. Do not generate a label solely to call this tool — if no label preference exists, use websites_create with no domain (auto-generates a platform subdomain) instead.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<label>",
		Args: []catalog.OperationArg{
			{Name: "label", Type: catalog.ArgTypeString, Required: true, Help: "Candidate subdomain label to check availability for. Required."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			label := catalog.StrArg(input, "label", "")
			if label == "" {
				return nil, errors.New("label is required: pass a candidate subdomain label to check availability")
			}
			// *ipfs.PlatformAvailabilityResponse
			return svc.CheckPlatformDomainAvailability(ctx, label)
		}),
	})
}
