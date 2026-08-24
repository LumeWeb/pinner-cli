package cli

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// catalog_websites_wiring.go adapts the websites domain operations in
// internal/catalogops to the urfave CLI: it compiles catalog operations into
// commands under the "websites" parent, renders each handler's result through
// the Output formatter, and maps positional args and the destructive --force
// gate onto operation inputs. IO and CLI concerns (positional <domain>
// mapping, force gate, update at-least-one-field gate, result rendering) live
// here, not in catalogops.
//
// Name mapping: canonical catalog names use dots ("websites.list"). The
// "websites." group prefix is stripped and leaves are mounted under a
// "websites" parent; the two-level op "websites.ssl.status" nests under an
// "ssl" parent command.
//
// The websites wizard and websites domains wizard commands are not compiled
// from the catalog (they drive an interactive stepwise session) and are
// appended to the websites parent as hand-written commands.

// catalogWebsitesDeps builds the catalogops.WebsitesDeps from the live CLI
// wiring. Service construction uses the core factories; all config is read
// lazily per invocation.
func catalogWebsitesDeps() catalogops.WebsitesDeps {
	return catalogops.WebsitesDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Secure: func() bool {
			cfgMgr, err := defaultConfigManagerFactory()
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
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

// websitesCatalogDepsVar is an indirection so the wiring and the renderer can
// both reach the canonical operation list without rebuilding it repeatedly.
var websitesCatalogDepsVar = catalogops.WebsitesDeps(catalogWebsitesDeps())

// newWebsitesCatalogCommands compiles the websites catalog operations and
// returns the top-level "websites" subcommands they produce (list, create,
// get, update, enable-ipns, delete, validate, ssl→status, config). The
// interactive hand-written commands (wizard, domains wizard) are appended by
// newWebsitesCommand.
func newWebsitesCatalogCommands() []*cli.Command {
	cat := catalog.NewCatalog()
	ops := catalogops.WebsitesOperations(websitesCatalogDepsVar)
	for _, op := range ops {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the websites group.
		panic(fmt.Sprintf("catalog compile websites: %v", err))
	}

	// Leaf commands compile flat with dotted names; nest the two-level ones
	// (websites.ssl.status -> ssl -> status) and the three-level ones
	// (websites.domains.dane.republish -> domains -> dane -> republish).
	parents := map[string]*cli.Command{}
	var out []*cli.Command
	for _, c := range compiled {
		mounted := mountWebsitesCatalogCommand(c)
		rest := strings.TrimPrefix(c.Name, "websites_")
		// Only nest genuine multi-level ops (ssl_status, domains_*).
		// websites_enable_ipns is a single-level leaf whose underscore is part
		// of the leaf name, so it must NOT be split (it would mount as
		// `websites enable ipns` and clobber the enable-ipns remap in
		// mountWebsitesCatalogCommand).
		if idx := strings.Index(rest, "_"); idx > 0 && rest != "enable_ipns" {
			// parent_child... : ssl_status -> ssl -> status, domains_list ->
			// domains -> list, domains_dane_republish -> domains -> dane ->
			// republish.
			parentName := rest[:idx]
			remainder := rest[idx+1:]
			parent, ok := parents[parentName]
			if !ok {
				parent = &cli.Command{Name: parentName, Category: "Management", Usage: "Manage website " + parentName, Commands: []*cli.Command{}}
				// The domains parent exposes a singular `domain` alias alongside
				// the canonical plural name.
				if parentName == "domains" {
					parent.Usage = "Manage domain bindings for a website"
					parent.Aliases = []string{"domain"}
					// The platform-domain concept is a single feature, so the
					// "platform" segment from websites_platform_domain_* renders
					// as the hyphenated "platform-domain" parent, matching the
					// CLI expectation (websites platform-domain availability).
				} else if parentName == "platform" {
					parent.Name = "platform-domain"
					parent.Usage = "Manage platform (free-subdomain) domain availability"
				}
				parents[parentName] = parent
				out = append(out, parent)
			}

			// The DANE republish op maps its dotted name to a three-level path:
			// websites.domains.dane.republish -> domains -> dane -> republish.
			if parentName == "domains" && remainder == "dane_republish" {
				var dane *cli.Command
				for _, sub := range parent.Commands {
					if sub.Name == "dane" {
						dane = sub
						break
					}
				}
				if dane == nil {
					dane = &cli.Command{Name: "dane", Category: "Management", Usage: "Manage a domain's DANE TLSA records", Commands: []*cli.Command{}}
					parent.Commands = append(parent.Commands, dane)
				}
				mounted.Name = "republish"
				dane.Commands = append(dane.Commands, mounted)
				continue
			}

			// The leaf keeps only its final segment when nested under a parent
			// (websites_ssl_status -> ssl -> status). Underscores in a leaf
			// name render as hyphens on the CLI (domains_dns_requirements ->
			// domains dns-requirements), matching the historical command names.
			mounted.Name = strings.ReplaceAll(remainder, "_", "-")
			// The domains leaves expose the conventional `rm`/`ls` aliases.
			if remainder == "remove" {
				mounted.Aliases = []string{"rm"}
			}
			if remainder == "list" {
				mounted.Aliases = []string{"ls"}
			}
			parent.Commands = append(parent.Commands, mounted)
			continue
		}
		out = append(out, mounted)
	}
	return out
}

// mountWebsitesCatalogCommand adapts a single catalog-compiled command
// (dotted name like "websites.list") into a live websites subcommand: it
// strips the "websites." group prefix, sets the category, applies the legacy
// CLI alias ("enable-ipns" is the canonical leaf; the CLI also exposed "ipns"),
// and wraps the Action with the CLI-input adapter and renderer.
func mountWebsitesCatalogCommand(cmd *cli.Command) *cli.Command {
	canonical := cmd.Name
	// Strip ONLY the "websites_" domain prefix. Two-level ops (ssl_status) keep
	// their intermediate segment (ssl_status) so newWebsitesCatalogCommands can
	// nest them under the "ssl" parent; only the websites_ group goes away here.
	display := strings.TrimPrefix(canonical, "websites_")
	// Keep the CLI leaf names stable: the renamed MCP tool websites_enable_ipns
	// still renders on the CLI as `websites enable-ipns` (hyphen) per its
	// historical CLI name.
	if display == "enable_ipns" {
		display = "enable-ipns"
	}
	cmd.Name = display
	cmd.Category = "Management"
	// Legacy alias: the websites group exposed `enable-ipns` with alias `ipns`.
	if display == "enable-ipns" {
		cmd.Aliases = []string{"ipns"}
	}

	var op catalog.Operation
	for _, cand := range catalogops.WebsitesOperations(websitesCatalogDepsVar) {
		if cand.Name() == canonical {
			op = cand
			break
		}
	}
	if op != nil {
		// Positional-supplied required args must not be urfave-parse-time
		// required (see relaxFlagRequired); call before wrapping the Action.
		relaxFlagRequired(cmd)
		// Preserve the legacy `ssl status --watch` presentational polling flag
		// (not part of the data contract — it lives here in the wiring layer).
		if canonical == "websites_ssl_status" {
			cmd.Flags = append(cmd.Flags, &cli.BoolFlag{Name: "watch", Usage: "Watch for SSL status changes"})
		}
		cmd.Action = websitesActionAdapter(op)
	}
	return cmd
}

// websitesActionAdapter returns the per-invocation ActionFunc for a websites
// catalog operation. It builds the operation input map from flags plus the
// resolved positional <domain>, applies the CLI-only gates (destructive
// --force for delete, at-least-one-field for update), and renders the
// handler's result through renderWebsitesResult.

// applyPositionalArgs maps supplied CLI positional arguments into the
// operation's declared string arguments by name, delegating the canonical
// mapping rule (right-aligned to declared <arg> slots, surplus rejection) to
// the catalog framework so every frontend interprets a Positional declaration
// identically. The adapter only translates the urfave args into a []string.
// Flag-populated values are never overwritten.
func applyPositionalArgs(op catalog.Operation, input map[string]any, args cli.Args) error {
	return catalog.MapPositionalArgs(op.Args(), op.Positional(), args.Slice(), input)
}

func websitesActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		output := setupOutput(c)

		// Build the input map from the compiler-declared flags.
		input := catalog.FlagsToInput(c, op)

		// Thread the per-invocation --auth-token flag into the operation's
		// service construction (flag -> config precedence, mirroring the
		// legacy GetAuthToken(c, cfgMgr)). Only set when provided so the
		// config-read fallback in deps.service() still applies otherwise.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional arguments into the operation's declared string
		// args by position order (op.Positional() describes them, e.g.
		// "<domain>", "[<website>] <domain>", or "<website>"). The catalog CLI
		// compiler reads flags only, so the adapter resolves any supplied
		// positionals into their declared input names.
		if err := applyPositionalArgs(op, input, c.Args()); err != nil {
			return err
		}

		// Destructive gate (websites delete). Enforce --force when a target is
		// present; with no target, fall through so the handler's required-arg
		// validation produces a non-zero exit instead of silently succeeding.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			input["confirm"] = confirm
			if !confirm && catalog.StrArg(input, "website", "") != "" {
				return fmt.Errorf("websites delete: pass --force to confirm this destructive operation")
			}
		}

		// At-least-one-field gate for update (mirrors requireUpdateFields).
		if op.Name() == "websites_update" {
			if !c.IsSet(FlagRenameTo) && !c.IsSet(FlagCID) && !c.IsSet(FlagTargetType) &&
				!c.IsSet(FlagDNSHosting) {
				return fmt.Errorf("at least one field must be provided for update (--rename-to, --cid, --target-type, --dns-hosting)")
			}
		}

		// Presentational watch loop for `ssl status --watch`: re-invoke the
		// handler (which returns the typed *ipfs.WebsiteResponse) and render
		// its SSL portion repeatedly. The data call stays in the handler; the
		// polling/formatting is CLI-IO, so it lives here.
		if op.Name() == "websites_ssl_status" && c.Bool("watch") {
			return output.Watch(ctx,
				func(ctx context.Context) (any, error) {
					return op.Handler().Execute(ctx, input)
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
						{"Status", website.Ssl.Status},
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

		// Apply the legacy per-call deadline (shared with every catalog domain).
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			renderVerifyGuidance(output, op, err)
			return err
		}
		return renderWebsitesResult(ctx, c, op, result)
	}
}

// renderVerifyGuidance renders actionable DNS self-service next-steps next to
// a `websites domains verify` error, so a failed/indeterminate validation tells
// the user what to do (mirrors the removed legacy handler). It renders only in
// human (non-JSON) output — in --json mode the error document stays machine
// clean, and it no-ops for every non-verify operation.
func renderVerifyGuidance(output Output, op catalog.Operation, err error) {
	if op.Name() == "websites_domains_verify" && !output.IsJSON() {
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
func renderWebsitesResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	// Guard against a typed-nil single-object result (interface non-nil,
	// underlying POINTER nil): a handler returning (nil, nil) yields a typed
	// nil here, and the pointer branches below would dereference it and panic.
	// Slices/maps are excluded — a nil slice legitimately means an empty
	// result set (e.g. `websites domains list` with no domains) and is handled
	// by the renderer's empty-state branches.
	if result != nil && isNilPointerResult(result) {
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case []ipfs.WebsiteItem:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": len(r), "websites": r})
		}
		if len(r) == 0 {
			output.Printfln("No websites found")
			return nil
		}
		output.Printfln("Found %d website(s)", len(r))
		headers := []string{"ID", "NAME", "CID", "RESOLVED CID", "STATUS", "DNS", "SUBDOMAIN", "GATEWAY", "VALIDATION", "CREATED"}
		rows := make([][]string, len(r))
		for i, w := range r {
			validation := ""
			if w.Status == "active" {
				validation = "validated"
			} else if w.Expired {
				validation = "expired"
			} else if w.ValidationToken != "" {
				validation = stripValidationPrefix(w.ValidationToken)
			}
			gateway := ""
			if w.GatewayDomain != nil {
				gateway = *w.GatewayDomain
			}
			resolvedCID := "-"
			if w.ActiveCid != nil {
				resolvedCID = *w.ActiveCid
			}
			rows[i] = []string{
				fmt.Sprintf("%d", w.Id), w.Domain, w.TargetHash, resolvedCID, w.Status,
				fmt.Sprintf("%t", w.DnsHostingEnabled), fmt.Sprintf("%t", w.IsSubdomain),
				gateway, validation, w.Created.Format("2006-01-02 15:04:05"),
			}
		}
		output.PrintTable(headers, rows)
		return nil

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
		rows := make([][]string, len(r))
		for i, d := range r {
			zoneName := ""
			if d.ZoneName != nil {
				zoneName = *d.ZoneName
			}
			status := ""
			if d.Status != nil {
				status = *d.Status
			}
			rows[i] = []string{
				fmt.Sprintf("%d", d.Id), d.Domain, d.Namespace, status, zoneName,
			}
		}
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
		if op.Name() == "websites_domains_dns_requirements" {
			renderDomainDelegation(output, r, r.DnsHostingEnabled)
			return nil
		}
		renderDomainResponse(output, r)
		return nil

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
		status = *r.Status
	}
	zoneName := ""
	if r.ZoneName != nil {
		zoneName = *r.ZoneName
	}
	fields := []Field{
		{"ID", fmt.Sprintf("%d", r.Id)},
		{"Domain", r.Domain},
		{"Namespace", r.Namespace},
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
		status = *r.Status
	}
	ownerName := ""
	if r.OwnerName != nil {
		ownerName = *r.OwnerName
	}
	tlsaRecord := ""
	if r.TlsaRecord != nil {
		tlsaRecord = *r.TlsaRecord
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", fmt.Sprintf("%d", r.Id)},
			{"Domain", r.Domain},
			{"Namespace", r.Namespace},
			{"Status", status},
			{"Owner Name", ownerName},
			{"TLSA Record", tlsaRecord},
		},
	})
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
			Field{"Validation Token", stripValidationPrefix(w.ValidationToken)},
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
		{"Status", r.Ssl.Status},
		{"Issued At", formatTimePtr(r.Ssl.IssuedAt)},
		{"Last Updated", formatTimePtr(r.Ssl.LastUpdatedAt)},
	}
	if r.Ssl.Error != nil && *r.Ssl.Error != "" {
		rows = append(rows, []string{"Error", *r.Ssl.Error})
	}
	output.PrintTable(headers, rows)
}
