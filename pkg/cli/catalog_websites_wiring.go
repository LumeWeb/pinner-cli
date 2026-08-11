package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// catalog_websites_wiring.go is the pkg/cli frontend adapter for the websites
// domain operations in internal/catalogops. It compiles the catalog's websites
// operations into real urfave/cli/v3 commands nested under the "websites"
// parent command, renders every handler's typed DATA result through the CLI's
// Output formatter, and maps positional args / the destructive force-gate onto
// the operation inputs.
//
// Architectural split (mirrors the pins pilot in catalog_wiring.go and the
// vault wiring in catalog_vault_wiring.go):
//   - internal/catalogops exposes WebsitesDeps + WebsitesOperations; it never
//     renders and never imports pkg/cli.
//   - This file is where IO/CLI concerns live: positional <domain> mapping,
//     the destructive --force gate for delete, the update at-least-one-field
//     gate, and all result rendering.
//
// Name mapping: canonical catalog names use dots ("websites.list"). We strip
// the "websites." group prefix and mount leaves under a "websites" parent; the
// two-level op "websites.ssl.status" is nested under an "ssl" parent command.
//
// The commands that are fundamentally interactive/IO (websites wizard,
// websites domains wizard) are NOT compiled from the catalog — the wizard
// commands drive an interactive stepwise session — and are appended to the
// websites parent as hand-written commands alongside the catalog ones.

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
	// (websites.ssl.status -> ssl -> status).
	parents := map[string]*cli.Command{}
	var out []*cli.Command
	for _, c := range compiled {
		mounted := mountWebsitesCatalogCommand(c)
		rest := strings.TrimPrefix(c.Name, "websites.")
		if idx := strings.Index(rest, "."); idx > 0 {
			// Two-level: parent.child (ssl.status)
			parentName := rest[:idx]
			parent, ok := parents[parentName]
			if !ok {
				parent = &cli.Command{Name: parentName, Category: "Management", Usage: "Manage website " + parentName, Commands: []*cli.Command{}}
				parents[parentName] = parent
				out = append(out, parent)
			}
			// The leaf keeps only its final segment when nested under a parent
			// (websites.ssl.status -> ssl -> status).
			mounted.Name = rest[idx+1:]
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
	// Strip ONLY the "websites." domain prefix. Two-level ops (ssl.status) keep
	// their intermediate segment (ssl.status) so newWebsitesCatalogCommands can
	// nest them under the "ssl" parent; only the websites. group goes away here.
	display := strings.TrimPrefix(canonical, "websites.")
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
		if canonical == "websites.ssl.status" {
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
func websitesActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		output := setupOutput(c)

		// Build the input map from the compiler-declared flags.
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// Thread the per-invocation --auth-token flag into the operation's
		// service construction (flag -> config precedence, mirroring the
		// legacy GetAuthToken(c, cfgMgr)). Only set when provided so the
		// config-read fallback in deps.service() still applies otherwise.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <domain> into the operation's "website" input.
		// The catalog CLI compiler reads flags only, so the adapter resolves
		// the positional <domain> into the declared string arg.
		if c.Args().Len() > 0 && hasArg(op, "website") {
			if stringVal(input["website"]) == "" {
				input["website"] = c.Args().First()
			}
		}

		// Destructive gate (websites delete). Enforce --force when a target is
		// present; with no target, fall through so the handler's required-arg
		// validation produces a non-zero exit instead of silently succeeding.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			if !confirm && stringVal(input["website"]) != "" {
				return fmt.Errorf("websites delete: pass --force to confirm this destructive operation")
			}
		}

		// At-least-one-field gate for update (mirrors requireUpdateFields).
		if op.Name() == "websites.update" {
			if !c.IsSet(FlagRenameTo) && !c.IsSet(FlagCID) && !c.IsSet(FlagTargetType) &&
				!c.IsSet(FlagDNSHosting) && !c.IsSet(FlagNoDNSHosting) {
				return fmt.Errorf("at least one field must be provided for update (--rename-to, --cid, --target-type, --dns-hosting, --no-dns-hosting)")
			}
		}

		// Presentational watch loop for `ssl status --watch`: re-invoke the
		// handler (which returns the typed *ipfs.WebsiteResponse) and render
		// its SSL portion repeatedly. The data call stays in the handler; the
		// polling/formatting is CLI-IO, so it lives here.
		if op.Name() == "websites.ssl.status" && c.Bool("watch") {
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

		result, err := op.Handler().Execute(ctx, input)
		if err != nil {
			return err
		}
		return renderWebsitesResult(ctx, c, op, result)
	}
}

// renderWebsitesResult is the catalog.RenderFunc that renders a websites
// handler's typed DATA result through the CLI Output formatter. It is the
// single rendering home for catalog-driven websites commands and never touches
// core services. JSON shapes are kept faithful to the legacy hand-written
// commands.
func renderWebsitesResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

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
		// websites ssl status returns a full WebsiteResponse; render the SSL
		// portion faithfully (the legacy ssl status command).
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

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// renderWebsiteItemHuman renders the fields of a single website (used by get,
// create, update, and enable-ipns), mirroring the legacy field presentation.
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
	if w.DnsZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *w.DnsZoneId)})
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

// renderWebsiteSSLStatusHuman renders the SSL portion of a WebsiteResponse,
// mirroring the legacy `websites ssl status` presentation.
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
