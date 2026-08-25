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

// dns_wiring.go adapts the DNS catalog operations
// (internal/catalogops/dns.go) to urfave/cli/v3 commands. The catalog never
// imports pkg/cli; this file maps CLI concerns (positional <domain> zone
// argument, the destructive --force gate for zone/record delete) onto the
// catalog and renders each handler's DATA result through the CLI Output
// formatter.
//
// The DNS operations are canonically dotted ("dns.zones.list",
// "dns.records.create", ...). The CLI nests them as "dns" -> ("zones" |
// "records") -> leaf. That nesting lives here, not in internal/catalog.
//
// The catalog compiler builds its input map from flags only. Commands that
// take a <domain> positionally (zones get/delete/validate, records
// list/create/get/update/delete) translate the first positional arg into the
// operation's "zone" input before dispatch. We do that by wrapping each
// compiled command's Action.

// catalogDNSDeps builds the catalogops.DNSDeps from the live CLI wiring.
// Service construction uses a discard writer so handlers return pure data and
// never render; all presentation happens in renderDNSResult.
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
			// Construct a service pinned to the override token; on failure fall
			// back to a token-less service so each handler's
			// RequireAuthenticated() returns ErrNotAuthenticated instead of
			// panicking on a nil service.
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

// dnsCatalogDeps holds the catalog's registered DNS operation deps.
var dnsCatalogDeps = catalogops.DNSDeps(catalogDNSDeps())

// newDNSCommand is the catalog-driven "dns" parent command. It compiles the
// DNS operations via the catalog's CLI compiler and nests the resulting leaf
// commands under "dns" -> ("zones" | "records").
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
		if !strings.HasPrefix(c.Name, "dns_") {
			continue // skip anything not in the dns domain
		}
		rest := strings.TrimPrefix(c.Name, "dns_")
		seg := strings.SplitN(rest, "_", 2)
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
	canonicalName := "dns_" + group + "_" + leaf

	return func(ctx context.Context, cmd *cli.Command) error {
		// Build the input map from the compiler-declared flags plus the
		// resolved positional <domain> in the "zone" input.
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

		input := catalog.FlagsToInput(cmd, op)

		// dns_records_update: disabled is an omitempty field on the wire, and
		// omitting it must leave the record's current disabled state unchanged.
		// flagValue returns false for an unset bool, so drop the key entirely
		// when the flag was not given; the handler then leaves it nil (unchanged)
		// instead of forcing re-enable.
		if canonicalName == "dns_records_update" && !cmd.IsSet(FlagDisabled) {
			delete(input, "disabled")
		}

		// The per-invocation --auth-token flag takes precedence over the config
		// token. Only set it when provided so deps.service() falls back to the
		// config-read GetAuthToken.
		if tok := cmd.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <domain>/<zone-id> into the "zone" input. The zone
		// arg is PositionalOnly on the DNS ops (no --zone flag), so the <domain>
		// positional is the only way to supply it.
		if cmd.Args().Len() > 0 {
			input["zone"] = cmd.Args().First()
		}

		// Destructive gate (zones delete, records delete). The catalog compiler
		// injects a --force flag; since we replace the Action we enforce it
		// ourselves, honoring both --force and the hidden --confirm alias.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := cmd.Bool(FlagForce) || cmd.Bool(FlagConfirm)
			input["confirm"] = confirm
			// With a target zone and no --force, refuse loudly (non-zero exit)
			// rather than silently succeeding; with no zone, fall through so the
			// handler's required-argument validation produces a non-zero exit.
			if !confirm && catalog.StrArg(input, "zone", "") != "" {
				return fmt.Errorf("dns %s: pass --force to confirm this destructive operation", leaf)
			}
		}

		// Apply the configured per-command timeout so a hanging backend fails
		// after the configured timeout instead of blocking.
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
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
	if z := catalog.StrArg(input, "zone", ""); z != "" {
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
	case catalogops.ListResult:
		return renderListResult(output, r)

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
