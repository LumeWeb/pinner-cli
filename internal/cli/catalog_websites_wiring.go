package cli

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/pinner/core/download"
	"go.lumeweb.com/pinner/core/websites"
)

// catalog_websites_wiring.go adapts the websites domain operations in
// internal/catalogops to the urfave CLI: it compiles the operations' command
// tree through the shape model in internal/clicatalog/shapes_websites.go and
// CompileCommandTree, renders each handler's result
// through the Output formatter, and maps positional args and the destructive
// --force gate onto operation inputs. IO and CLI concerns (positional <domain>
// mapping, force gate, update at-least-one-field gate, result rendering,
// ssl status --watch) live here, not in catalogops.
//
// Name mapping: canonical catalog names are underscore-separated
// ("websites_list"); nesting, aliases and naming are declared in
// shapes_websites.go (websites_ssl_status -> ssl -> status, websites_domains_*
// under a "domains" parent, websites_platform_domain* under a
// "platform-domain" parent, websites_enable_ipns -> "enable-ipns" with the
// legacy "ipns" alias), not inferred here. Only the websites-specific leaf
// behavior (the --watch flag and the shared catalog action adapter) stays
// mount-owned in buildWebsitesLeaf and websitesCatalogConfig.
//
// The websites wizard and websites domains wizard commands are not compiled
// from the catalog (they drive an interactive stepwise session) and are
// appended to the websites parent as hand-written commands.

// catalogWebsitesDeps builds the catalogops.WebsitesDeps from the live CLI
// wiring. Service construction uses the core factories; all config is read
// lazily per invocation.
func catalogWebsitesDeps(factory ...ConfigManagerFactory) catalogops.WebsitesDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.WebsitesDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Secure: func() bool {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return false
			}
			return GetSecureSetting(nil, cfgMgr)
		},
		ServiceFactory: websites.DefaultFactory,
		NewAuthenticated: func(cfgMgr config.Manager, secure bool, token string) (websites.Service, error) {
			return websites.NewAuthenticated(cfgMgr, token, secure)
		},
		GetAuthToken: func() string {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
		DownloadServiceFactory: func(cfgMgr config.Manager, secure bool, authToken string) (download.Service, error) {
			opts := []DownloadServiceOption{
				WithDownloadIPFSEndpoint(cfgMgr.Config().GetIPFSEndpointWithSecure(secure)),
			}
			if authToken != "" {
				opts = append(opts, WithDownloadAuthToken(authToken))
			}
			return NewDownloadService(cfgMgr, NewOutputFormatter(false, false, true, false), opts...), nil
		},
		IPNSResolveFunc: func(ctx context.Context, name, authToken string) (string, error) {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return "", err
			}
			secure := GetSecureSetting(nil, cfgMgr)
			svc, err := newIPNSAPI(cfgMgr, authToken, secure)
			if err != nil {
				return "", err
			}
			resp, err := svc.Resolve(ctx, name)
			if err != nil {
				return "", err
			}
			return resp.Value, nil
		},
	}
}

// websitesCatalogDepsVar is an indirection so the wiring and the renderer can
// both reach the canonical operation list without rebuilding it repeatedly.
var websitesCatalogDepsVar = catalogops.WebsitesDeps(catalogWebsitesDeps())

// newWebsitesCatalogCommands compiles the websites catalog operations and
// returns the top-level "websites" subcommands they produce (list, create,
// get, update, enable-ipns, delete, validate, ssl→status, config) plus the
// synthesized parents (domains, platform-domain). It declares the operations'
// command shape in internal/clicatalog/shapes_websites.go and materializes the
// tree through mount-owned leaf and parent builders. The interactive
// hand-written commands (wizard, domains wizard) are appended by
// newWebsitesCommand.
func newWebsitesCatalogCommands() []*cli.Command {
	root := clicatalog.WebsitesDomainRoot
	ops := catalogops.WebsitesOperations(websitesCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.WebsitesShapes,
		root,
		websitesCatalogConfig(),
		buildWebsitesLeaf,
		buildCLIParent,
	)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the websites group.
		panic(fmt.Sprintf("catalog compile websites: %v", err))
	}
	return cmds
}

// buildWebsitesLeaf is the mount-owned leaf builder materializing one websites
// leaf into an urfave *cli.Command. Shape (name/category/aliases/flags/usage)
// comes from the clicatalog model via NewCLILeaf; behavior (the catalog action
// adapter + relaxFlagRequired) stays mount-owned here. The one
// websites-specific addition beyond the generic leaf builder is the
// legacy `ssl status --watch` presentational polling flag (not part of the
// data contract — it lives here in the wiring layer).
func buildWebsitesLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	if loc.Op.Name() == "websites_ssl_status" {
		base.Flags = append(base.Flags, &cli.BoolFlag{Name: "watch", Usage: "Watch for SSL status changes"})
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// websitesCatalogConfig returns the CatalogAdapterConfig that expresses the
// websites domain's exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline. It mirrors the former per-domain websites
// adapter faithfully: the result renderer, the per-invocation --auth-token
// override,
// the canonical positional mapping, the destructive --force gate (websites
// delete only), the at-least-one-field update guard, the ssl status --watch
// post-normalize loop, and the domains-verify error guidance. All remaining
// fields stay nil so the shared pipeline's safe defaults apply.
func websitesCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderWebsitesResult,
		HonorAuthTokenOverride: true,

		// Destructive gate (websites delete). The shared pipeline enforces
		// --force/--confirm; with a target website and no --force, refuse
		// loudly (non-zero exit) with the "websites delete:" message. With no
		// target (no "website" arg), fall through so the handler's required-arg
		// validation produces a non-zero exit. `websites domains
		// convert-onchain` is also SafetyDestructive but declares a "domain"
		// arg (no "website"), so StrArg(input, "website", "") stays empty and
		// it falls through to the handler's own confirm check — never gated
		// here.
		DestructiveGate: GateForceReject(
			func(ic *CatalogInvokeContext) bool {
				return opmesh.StrArg(ic.Input, "website", "") != ""
			},
			func(ic *CatalogInvokeContext) string {
				return "websites delete: pass --force to confirm this destructive operation"
			},
		),

		// At-least-one-field gate for update (mirrors requireUpdateFields).
		UpdateGuard: func(ic *CatalogInvokeContext) error {
			if ic.Op.Name() == "websites_update" {
				c := ic.C
				if !c.IsSet(FlagRenameTo) && !c.IsSet(FlagCID) && !c.IsSet(FlagTargetType) &&
					!c.IsSet(FlagDNSHosting) {
					return fmt.Errorf("at least one field must be provided for update (--rename-to, --cid, --target-type, --dns-hosting)")
				}
			}
			return nil
		},

		// ssl status --watch: presentational poll loop that re-invokes the
		// handler on the NORMALIZED input (the data call stays in the
		// handler; the polling/formatting is CLI-IO). It runs as a
		// PostNormalizeWatch hook so normalize computes once and both the
		// single-execute and the watch re-execute paths share the same input.
		// handled=true stops the pipeline before Execute/Render, exactly as the
		// old adapter returned the Watch result directly.
		PostNormalizeWatch: func(ic *CatalogInvokeContext, normalized map[string]any) (bool, error) {
			if ic.Op.Name() == "websites_ssl_status" && ic.C.Bool(FlagWatch) {
				return true, ic.Output.Watch(ic.Ctx,
					func(ctx context.Context) (any, error) {
						return ic.Op.Handler().Execute(ctx, normalized)
					},
					func(data any) (string, []string, [][]string) {
						website, ok := data.(*ipfs.WebsiteResponse)
						if !ok || website == nil {
							return "SSL Status - No data", nil, nil
						}
						title := fmt.Sprintf("SSL Status for %s", website.Domain)
						if website.Ssl == nil {
							return title + "\n  No SSL information available", nil, nil
						}
						headers := []string{"Field", "Value"}
						rows := [][]string{
							{"Status", string(website.Ssl.Status)},
							{"Issued At", formatTimePtr(website.Ssl.IssuedAt)},
							{"Last Updated", formatTimePtr(website.Ssl.LastUpdatedAt)},
						}
						if website.Ssl.Error != nil && *website.Ssl.Error != "" {
							rows = append(rows, []string{"Error", *website.Ssl.Error})
						}
						return title, headers, rows
					},
				)
			}
			return false, nil
		},

		// On execute failure, print DNS self-service guidance for the
		// websites_domains_verify op (human output only) and return the error
		// unchanged.
		OnExecuteError: func(ic *CatalogInvokeContext, err error) error {
			renderVerifyGuidance(ic.Output, ic.Op, err)
			return err
		},
	}
}

// renderVerifyGuidance renders actionable DNS self-service next-steps next to
// a `websites domains verify` error, so a failed/indeterminate validation tells
// the user what to do (mirrors the removed legacy handler). It renders only in
// human (non-JSON) output — in --json mode the error document stays machine
// clean, and it no-ops for every non-verify operation.
func renderVerifyGuidance(output Output, op opmesh.Operation, err error) {
	if op.Name() == catalogops.OpWebsitesDomainsVerify && !output.IsJSON() {
		renderDNSSelfServiceGuidance(output, err)
	}
}

// isNilPointerResult reports whether v is a non-nil interface wrapping a nil
// POINTER (a typed nil pointer). It deliberately ignores slice/map/chan/func
// kinds: a nil slice or map is a legitimate empty result, not a nil-pointer
// deref hazard. Used to guard renderers that dereference single-object
// pointer results against handlers that return (nil, nil).
func isNilPointerResult(v any) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		return rv.IsNil()
	}
	return false
}

// renderWebsitesResult is the catalog.RenderFunc that renders a websites
// handler's typed result through the CLI Output formatter. It is the single
// rendering home for catalog-driven websites commands.
func renderWebsitesResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
	output := setupOutput(c)

	// Guard against a typed-nil single-object result (interface non-nil,
	// underlying POINTER nil): a handler returning (nil, nil) yields a typed
	// nil here, and the pointer branches below would dereference it and panic.
	// Slices/maps are excluded — a nil slice legitimately means an empty
	// result set (e.g. `websites domains list` with no domains) and is handled
	// by the renderer's empty-state branches.
	// A verify returning (nil, nil) is meaningful: DNS is not resolvable yet,
	// so render the not-verified outcome rather than bailing out.
	if result != nil && isNilPointerResult(result) {
		if op.Name() == catalogops.OpWebsitesDomainsVerify {
			renderDomainVerifyResult(output, nil)
			return nil
		}
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

	case *ipfs.WebsiteItem:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		renderWebsiteItemHuman(output, r)
		return nil

	case *ipfs.WebsiteResponse:
		// websites ssl status returns a full WebsiteResponse; render its SSL
		// portion.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		renderWebsiteSSLStatusHuman(output, r)
		return nil

	case *ipfs.WebsiteValidateResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		statusIcon := "⏳"
		if r.Valid {
			statusIcon = "✅"
		}
		output.Printfln("Website Validation Result")
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Domain", r.Domain},
				{"ID", fmt.Sprintf("%d", r.Id)},
				{"Valid", fmt.Sprintf("%s %t", statusIcon, r.Valid)},
				{"Message", r.Message},
			},
		})
		renderValidationChecks(output, r.Checks)
		return nil

	case *ipfs.WebsiteConfigResponse:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Website Hosting Configuration")
		fields := []Field{}
		if r.GatewayDomain != nil && *r.GatewayDomain != "" {
			fields = append(fields, Field{"Gateway Domain", *r.GatewayDomain})
		}
		if r.Nameservers != nil && len(*r.Nameservers) > 0 {
			fields = append(fields, Field{"Nameservers", strings.Join(*r.Nameservers, ", ")})
		}
		if len(fields) > 0 {
			output.PrintFields(FieldGroup{Fields: fields})
		}
		if len(fields) == 0 {
			output.Printfln("  No gateway domain or nameservers configured")
		}
		return nil

	case *catalogops.WebsiteDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"success": true, "message": fmt.Sprintf("Website %s deleted successfully", r.ID)})
		}
		output.Printfln("Website deleted successfully")
		return nil

	case *catalogops.DomainDNSRequirements:
		// websites domains dns-requirements: the domain response plus the
		// owning website, from which the renderer derives the authoritative
		// records the backend no longer returns for on-chain bindings.
		// --json keeps the historical domain-response shape.
		if r.Domain == nil {
			return fmt.Errorf("no result returned for %s", op.Name())
		}
		if output.IsJSON() {
			return output.PrintJSON(r.Domain)
		}
		renderDomainDelegation(output, r.Domain, r.Domain.DnsHostingEnabled, r.Website)
		return nil

	case []ipfs.DomainResponse:
		// websites domains list: a website's domain bindings.
		if output.IsJSON() {
			if r == nil {
				r = []ipfs.DomainResponse{}
			}
			return output.PrintJSON(map[string]any{"count": len(r), "domains": r})
		}
		if len(r) == 0 {
			output.Printfln("No domains found")
			return nil
		}
		output.Printfln("Found %d domain(s)", len(r))
		headers := []string{"ID", "DOMAIN", "NAMESPACE", "STATUS", "ZONE NAME"}
		rows := lo.Map(r, func(d ipfs.DomainResponse, _ int) []string {
			zoneName := ""
			if d.ZoneName != nil {
				zoneName = *d.ZoneName
			}
			status := ""
			if d.Status != nil {
				status = string(*d.Status)
			}
			return []string{
				fmt.Sprintf("%d", d.Id), d.Domain, string(d.Namespace), status, zoneName,
			}
		})
		output.PrintTable(headers, rows)
		return nil

	case *ipfs.DomainResponse:
		// websites domains add/verify/update/dns-requirements all return a
		// DomainResponse. The dns-requirements command renders the delegation
		// bundle; the others render the binding fields. Guard against a typed
		// nil before dereferencing (mirrors the removed legacy checks).
		if r == nil {
			return fmt.Errorf("no result returned for %s", op.Name())
		}
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		switch op.Name() {
		case catalogops.OpWebsitesDomainsVerify:
			renderDomainVerifyResult(output, r)
			return nil
		case catalogops.OpWebsitesDomainsDNSRequirements:
			// Version skew: when the dns-requirements op returns a plain
			// DomainResponse instead of the wrapper, its delegation/validation
			// rendering must still reach the user (mirrors the merged
			// OpWebsitesDomainsVerify routing below). This renderer is a pure
			// function (no service seam to resolve the owning website), so the
			// on-chain delegation driver cannot derive the _dnslink record that
			// depends on the website's target. What IS derivable without the
			// website still renders: the on-chain TLSA table falls back to the
			// response's own TlsaRdata when the delegation bundle is absent, so
			// the version-skew path is not empty. The nil website is therefore a
			// deliberate best-effort (matches the upstream wrapper only where the
			// data it needs is present on the bare response).
			renderDomainDelegation(output, r, r.DnsHostingEnabled, nil)
			return nil
		default:
			renderDomainResponse(output, r)
			return nil
		}

	case *ipfs.DomainDANERepublishResponse:
		// websites domains dane republish.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		renderDomainDANEResponse(output, r)
		return nil

	case *catalogops.WebsiteDomainsRemoveResult:
		// websites domains remove.
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "domain_id": r.DomainID})
		}
		output.Printfln("Domain removed successfully")
		return nil

	case *ipfs.PlatformDomainListResponse:
		// websites platform-domains list: enabled platform (free-subdomain)
		// roots available for users to claim subdomains under.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Platform Domains")
		if len(r.Data) == 0 {
			output.Printfln("  No platform domains found")
			return nil
		}
		headers := []string{"ID", "DOMAIN", "NAMESPACE", "ZONE ID", "ENABLED"}
		rows := lo.Map(r.Data, func(res ipfs.PlatformDomainResponse, _ int) []string {
			return []string{
				fmt.Sprintf("%d", res.Id),
				res.Domain,
				res.Namespace,
				fmt.Sprintf("%d", res.ZoneId),
				fmt.Sprintf("%t", res.Enabled),
			}
		})
		output.PrintTable(headers, rows)
		return nil

	case *ipfs.PlatformAvailabilityResponse:
		// websites platform-domain availability: a label plus one availability
		// result per enabled platform (free-subdomain) root.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Platform Domain Availability")
		if r.Label != "" {
			output.Printfln("Label: %s", r.Label)
		}
		if len(r.Results) == 0 {
			output.Printfln("  No platform domains found")
			return nil
		}
		headers := []string{"PLATFORM DOMAIN", "NAMESPACE", "AVAILABLE"}
		rows := lo.Map(r.Results, func(res ipfs.PlatformAvailabilityResult, _ int) []string {
			return []string{res.PlatformDomain, res.Namespace, fmt.Sprintf("%t", res.Available)}
		})
		output.PrintTable(headers, rows)
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// renderDomainResponse renders the fields of a single domain binding (used by
// websites domains add/verify/update).
func renderDomainResponse(output Output, r *ipfs.DomainResponse) {
	status := ""
	if r.Status != nil {
		status = string(*r.Status)
	}
	zoneName := ""
	if r.ZoneName != nil {
		zoneName = *r.ZoneName
	}
	fields := []Field{
		{"ID", fmt.Sprintf("%d", r.Id)},
		{"Domain", r.Domain},
		{"Namespace", string(r.Namespace)},
		{"Status", status},
		{"Zone Name", zoneName},
		// Surface the per-domain DNS hosting state so the user can verify
		// --dns-hosting on update actually applied. (The API does not echo the
		// primary flag back in DomainResponse, so it cannot be rendered here.)
		{"DNS Hosting", fmt.Sprintf("%v", r.DnsHostingEnabled)},
	}
	if r.Delegation != nil && r.Delegation.Dnssec != nil {
		fields = append(fields, Field{"DNSSEC", *r.Delegation.Dnssec})
		if r.Delegation.DnssecError != nil && *r.Delegation.DnssecError != "" {
			fields = append(fields, Field{"DNSSEC Error", *r.Delegation.DnssecError})
		}
	}
	output.PrintFields(FieldGroup{Fields: fields})
}

// renderDomainDANEResponse renders the result of websites domains dane
// republish.
func renderDomainDANEResponse(output Output, r *ipfs.DomainDANERepublishResponse) {
	status := ""
	if r.Status != nil {
		status = string(*r.Status)
	}
	ownerName := ""
	if r.OwnerName != nil {
		ownerName = *r.OwnerName
	}
	fields := []Field{
		{"ID", fmt.Sprintf("%d", r.Id)},
		{"Domain", r.Domain},
		{"Namespace", string(r.Namespace)},
		{"Status", status},
		{"Owner Name", ownerName},
		// published_to_managed_zone is a required field and always echoed; a
		// false value means the republished TLSA is NOT live in the managed
		// zone yet, so the user must not treat the command as a success.
		{"TLSA Published", fmt.Sprintf("%t", r.PublishedToManagedZone)},
	}
	if r.TlsaRdata != nil && *r.TlsaRdata != "" {
		fields = append(fields, Field{"TLSA Record", *r.TlsaRdata})
	}
	output.PrintFields(FieldGroup{Fields: fields})
}

// renderWebsiteItemHuman renders the fields of a single website (used by get,
// create, update, and enable-ipns).
func renderWebsiteItemHuman(output Output, w *ipfs.WebsiteItem) {
	output.Printfln("Website Details")

	fields := []Field{
		{"ID", fmt.Sprintf("%d", w.Id)},
		{"Domain", w.Domain},
		{"CID", w.TargetHash},
		{"Target Type", w.TargetType},
		{"Status", w.Status},
		{"DNS Hosting", fmt.Sprintf("%t", w.DnsHostingEnabled)},
		{"Subdomain", fmt.Sprintf("%t", w.IsSubdomain)},
	}
	if w.ActiveCid != nil {
		fields = append(fields, Field{"Resolved CID", *w.ActiveCid})
	}
	if w.Status != "active" {
		fields = append(fields,
			Field{"Token Expired", fmt.Sprintf("%t", w.Expired)},
			Field{"Validation Token", websites.StripValidationPrefix(w.ValidationToken)},
		)
		if w.ValidationExpiresAt != nil {
			fields = append(fields, Field{"Token Expires", w.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
		}
	}
	if w.GatewayDomain != nil {
		fields = append(fields, Field{"Gateway", *w.GatewayDomain})
	}
	if w.IpnsKeyId != nil {
		fields = append(fields, Field{"IPNS Key ID", fmt.Sprintf("%d", *w.IpnsKeyId)})
	}
	if w.ZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *w.ZoneId)})
	}
	if w.ValidationRecordHost != nil && *w.ValidationRecordHost != "" {
		fields = append(fields, Field{"Validation Host", *w.ValidationRecordHost})
	}
	fields = append(fields, Field{"Created", w.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

	if w.Expired && w.Status != "active" {
		output.Printfln("")
		output.Printfln("⚠ Validation token has expired. Re-validate to generate a new token:")
		output.Printfln("  pinner websites validate %d", w.Id)
	}
}

// renderWebsiteSSLStatusHuman renders the SSL portion of a WebsiteResponse.
func renderWebsiteSSLStatusHuman(output Output, r *ipfs.WebsiteResponse) {
	output.Printfln("SSL Status for %s", r.Domain)
	if r.Ssl == nil {
		output.Printfln("  No SSL information available")
		return
	}
	headers := []string{"Field", "Value"}
	rows := [][]string{
		{"Status", string(r.Ssl.Status)},
		{"Issued At", formatTimePtr(r.Ssl.IssuedAt)},
		{"Last Updated", formatTimePtr(r.Ssl.LastUpdatedAt)},
	}
	if r.Ssl.Error != nil && *r.Ssl.Error != "" {
		rows = append(rows, []string{"Error", *r.Ssl.Error})
	}
	output.PrintTable(headers, rows)
}
