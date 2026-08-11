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
	"go.lumeweb.com/pinner-cli/internal/core/dns"
)

// dns_wiring.go is the pkg/cli frontend adapter for the DNS catalog
// operations (internal/catalogops/dns.go). It keeps the catalog fully free of
// pkg/cli imports: this file is the single place that maps IO/CLI concerns
// (positional <domain> zone argument, the destructive --force gate for
// zone/record delete) onto the catalog and renders every handler's typed DATA
// result through the CLI Output formatter.
//
// Name mapping: the DNS operations are canonically dotted
// ("dns.zones.list", "dns.records.create", ...). The real CLI nests them as
// "dns" → ("zones" | "records") → leaf. This two-level nesting lives HERE,
// not in internal/catalog.
//
// Positional mapping: the catalog compiler builds its input map from FLAGS
// only. Commands that take a <domain> positionally (zones get/delete/validate,
// records list/create/get/update/delete) therefore need the wiring layer to
// translate the first positional arg into the operation's "zone" input before
// dispatching. We do that here by wrapping each compiled command's Action.

// catalogDNSDeps builds the catalogops.DNSDeps from the live CLI wiring.
// Service construction uses a discard writer so handlers return pure data and
// NEVER render; all presentation happens in renderDNSResult.
func catalogDNSDeps() catalogops.DNSDeps {
	return catalogops.DNSDeps{
		// Lazy config manager: resolved per invocation, never at package init.
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
		ServiceFactory: dns.ServiceFactory,
		NewAuthenticated: func(cfgMgr config.Manager, secure bool, token string) dns.Service {
			// Mirrors newDNSAPI (dns.NewAuthenticated). Constructs a service
			// pinned to the override token; on failure fall back to a TOKEN-LESS
			// service so every handler's RequireAuthenticated() returns
			// ErrNotAuthenticated cleanly instead of panicking on a nil service.
			svc, err := dns.NewAuthenticated(cfgMgr, token, secure)
			if err != nil {
				return dns.ServiceFactory(cfgMgr, secure)
			}
			return svc
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

// dnsCatalogDeps lazily builds the catalog's registered DNS operations.
var dnsCatalogDeps = catalogops.DNSDeps(catalogDNSDeps())

// newDNSCommand is the catalog-driven "dns" parent command. It compiles the
// DNS operations via the catalog's CLI compiler (NewCLICompilerWithRenderer)
// and nests the resulting leaf commands under "dns" → ("zones" | "records").
// This replaces the hand-written DNS command tree; the hand-written dnsXxx
// handler functions in dns.go are retained for their tests.
func newDNSCommand() *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.DNSOperations(dnsCatalogDeps) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile dns: %v", err))
	}

	// Group compiled leaf commands by their second dotted segment
	// ("zones" | "records") so we can nest them under the dns parent.
	leaves := compiled
	groups := map[string][]*cli.Command{}
	for _, c := range leaves {
		if !strings.HasPrefix(c.Name, "dns.") {
			continue // skip anything not in the dns domain
		}
		rest := strings.TrimPrefix(c.Name, "dns.")
		rest = strings.TrimPrefix(rest, ".")
		seg := strings.SplitN(rest, ".", 2)
		if len(seg) != 2 {
			continue
		}
		group, leaf := seg[0], seg[1]
		c.Name = leaf
		c.Category = "DNS"
		// Positional-supplied required args must not be urfave-parse-time
		// required (see relaxFlagRequired); call before wrapping the Action.
		relaxFlagRequired(c)
		c.Action = dnsCatalogActionAdapter(c, group, leaf)
		groups[group] = append(groups[group], c)
	}

	subCommands := make([]*cli.Command, 0, 2)
	for _, group := range []string{"zones", "records"} {
		cmds := groups[group]
		if len(cmds) == 0 {
			continue
		}
		usage := "Manage DNS zones"
		desc := "Manage DNS zones, the containers that hold DNS records for a domain."
		if group == "records" {
			usage = "Manage DNS records"
			desc = "Manage DNS records (A, AAAA, CNAME, TXT, MX, NS) inside an existing DNS zone."
		}
		subCommands = append(subCommands, &cli.Command{
			Name:        group,
			Category:    "DNS",
			Usage:       usage,
			Description: desc,
			Commands:    cmds,
		})
	}

	return &cli.Command{
		Name:        "dns",
		Category:    "Management",
		Usage:       "Manage DNS zones and records",
		Description: "Manage raw DNS zones and records for your domains (A/AAAA/CNAME/TXT/MX/NS, _dnslink, apex vs subdomain). Zones hold records; create the zone first ('dns zones create'), then manage records in it ('dns records *'). These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
		Commands:    subCommands,
	}
}

// dnsCatalogActionAdapter returns the per-invocation ActionFunc for a DNS
// catalog operation. It resolves the positional <domain> into the operation's
// "zone" input, enforces the destructive --force gate for delete operations,
// then invokes the handler and renders the result via renderDNSResult.
func dnsCatalogActionAdapter(c *cli.Command, group, leaf string) cli.ActionFunc {
	canonicalName := "dns." + group + "." + leaf

	return func(ctx context.Context, cmd *cli.Command) error {
		// Build the input map from the compiler-declared flags plus the
		// resolved positional <domain> → "zone" input.
		var op catalog.Operation
		for _, cand := range catalogops.DNSOperations(dnsCatalogDeps) {
			if cand.Name() == canonicalName {
				op = cand
				break
			}
		}
		if op == nil {
			return fmt.Errorf("catalog command %q not found", canonicalName)
		}

		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(cmd, a)
		}

		// Thread the per-invocation --auth-token flag into the operation's
		// service construction (flag -> config precedence). Only when set, so
		// deps.service() still falls back to the config-read GetAuthToken.
		if tok := cmd.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <domain>/<zone-id> into the "zone" input.
		if cmd.Args().Len() > 0 && stringVal(input["zone"]) == "" {
			input["zone"] = cmd.Args().First()
		}

		// Destructive gate (zones delete, records delete). The catalog
		// compiler injects a --force flag; since we replace the Action we
		// enforce it ourselves, honoring both --force and the hidden
		// --confirm legacy alias.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := cmd.Bool(FlagForce) || cmd.Bool(FlagConfirm)
			input["confirm"] = confirm
			// With a target zone and no --force, refuse loudly (non-zero exit)
			// rather than silently succeeding; with no zone, fall through so the
			// handler's required-argument validation produces a non-zero exit.
			if !confirm && stringVal(input["zone"]) != "" {
				return fmt.Errorf("dns %s: pass --force to confirm this destructive operation", leaf)
			}
		}

		// Restore the legacy per-call deadline: the migrated catalog handlers
		// call the DNS service with the raw context, but legacy dns actions
		// wrapped it in context.WithTimeout(GetDefaultTimeout) so a hanging
		// backend fails after the configured timeout instead of blocking forever.
		if cfgMgr, err := defaultConfigManagerFactory(); err == nil && cfgMgr != nil {
			timeout := cfgMgr.Config().GetDefaultTimeout()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
		}

		result, err := op.Handler().Execute(ctx, input)
		if err != nil {
			return err
		}
		return renderDNSResult(ctx, cmd, op, result)
	}
}

// describeDNSAction produces a human phrase describing what a destructive DNS
// operation would have done (used by the --force guard hint).
func describeDNSAction(group, leaf string, input map[string]any) string {
	what := "the " + group + " " + leaf + " operation"
	if z := stringVal(input["zone"]); z != "" {
		what = fmt.Sprintf("%s on %s", what, z)
	}
	return what
}

// renderDNSResult is the catalog.RenderFunc that renders a DNS handler's typed
// DATA result through the CLI Output formatter. It is the single rendering
// home for catalog-driven DNS commands and never touches core services.
func renderDNSResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case []ipfs.ZoneListResponse:
		if len(r) == 0 {
			output.Printfln("No DNS zones found")
			return nil
		}
		output.Printfln("DNS Zones:")
		for i, zone := range r {
			fields := []Field{
				{"ID", fmt.Sprintf("%d", zone.Id)},
				{"Domain", zone.Domain},
				{"Status", zone.Status},
			}
			if zone.PowerdnsZoneId != nil {
				fields = append(fields, Field{"PowerDNS Zone ID", *zone.PowerdnsZoneId})
			}
			fields = append(fields, Field{"Created", zone.CreatedAt.Format("2006-01-02 15:04:05")})
			output.PrintFields(FieldGroup{Fields: fields, PadTop: i})
		}
		return nil

	case *ipfs.ZoneResponse:
		fields := []Field{
			{"ID", fmt.Sprintf("%d", r.Id)},
			{"Domain", r.Domain},
			{"Status", r.Status},
		}
		if r.PowerdnsZoneId != nil {
			fields = append(fields, Field{"PowerDNS Zone ID", *r.PowerdnsZoneId})
		}
		fields = append(fields,
			Field{"Created", r.CreatedAt.Format("2006-01-02 15:04:05")},
			Field{"Updated", r.UpdatedAt.Format("2006-01-02 15:04:05")},
		)
		output.PrintFields(FieldGroup{Title: "DNS Zone Details:", Fields: fields})
		return nil

	case *ipfs.ValidationResponse:
		statusIcon := "⏳"
		if r.Valid {
			statusIcon = "✅"
		}
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Valid", fmt.Sprintf("%s %t", statusIcon, r.Valid)},
				{"Message", r.Message},
				{"Checked At", r.CheckedAt.Format("2006-01-02 15:04:05")},
			},
		})
		if !r.Valid {
			output.Printfln("")
			output.Printfln("Next steps:")
			if r.Nameservers != nil && len(*r.Nameservers) > 0 {
				output.Printfln("  Update your domain's nameservers at your registrar to:")
				for _, ns := range *r.Nameservers {
					output.Printfln("    - %s", ns)
				}
			} else {
				output.Printfln("  Check that your domain's nameservers are properly delegated to Pinner.xyz")
			}
		}
		return nil

	case []ipfs.RecordResponse:
		if len(r) == 0 {
			output.Printfln("No DNS records found")
			return nil
		}
		output.Printfln("DNS Records:")
		headers, rows := dnsRecordsTable(r)
		output.PrintTable(headers, rows)
		return nil

	case *ipfs.RecordResponse:
		fields := []Field{
			{"Zone ID", fmt.Sprintf("%d", r.ZoneId)},
			{"Name", r.Name},
			{"Type", r.Type},
			{"Content", r.Content},
			{"TTL", fmt.Sprintf("%d", r.Ttl)},
		}
		if r.Disabled {
			fields = append(fields, Field{"Status", "disabled"})
		}
		output.PrintFields(FieldGroup{Title: "DNS Record Details:", Fields: fields})
		return nil

	case *catalogops.DNSZoneDeleteResult:
		output.Printfln("DNS zone %s deleted successfully", r.Zone)
		return nil

	case *catalogops.DNSRecordDeleteResult:
		output.Printfln("DNS record deleted successfully")
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
