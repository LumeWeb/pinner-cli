package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/pinner/core/dns"
)

// dns_wiring.go adapts the DNS catalog operations
// (internal/catalogops/dns.go) to urfave/cli/v3 commands. The catalog never
// imports pkg/cli; this file maps CLI concerns (positional <domain> zone
// argument, the destructive --force gate for zone/record delete) onto the
// catalog and renders each handler's data result through the CLI Output
// formatter.
//
// The DNS operations are canonically underscore-separated ("dns_zones_list",
// "dns_records_create", ...). The CLI nests them as "dns" -> ("zones" |
// "records") -> leaf. That nesting is declared in internal/clicatalog/shapes_dns.go,
// not hardcoded here.
//
// The catalog compiler builds its input map from flags only. Commands that
// take a <domain> positionally (zones get/delete/validate, records
// list/create/get/update/delete) translate the first positional arg into the
// operation's "zone" input before dispatch. We do that by wrapping each
// compiled command's Action.

// catalogDNSDeps builds the catalogops.DNSDeps from the live CLI wiring.
// Service construction uses a discard writer so handlers return pure data and
// never render; all presentation happens in renderDNSResult.
func catalogDNSDeps(factory ...ConfigManagerFactory) catalogops.DNSDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.DNSDeps{
		// Lazy config manager: resolved per invocation, never at package init.
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
			cfgMgr, err := cfgFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

// dnsCatalogDeps holds the catalog's registered DNS operation deps.
var dnsCatalogDeps = catalogops.DNSDeps(catalogDNSDeps())

// newDNSCommand is the catalog-driven "dns" parent command. It declares the
// DNS operations' command shape in internal/clicatalog/shapes_dns.go and
// materializes the tree through mount-owned leaf and parent builders.
func newDNSCommand() *cli.Command {
	root := clicatalog.DNSDomainRoot
	ops := catalogops.DNSOperations(dnsCatalogDeps)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.DNSShapes,
		root,
		dnsCatalogConfig(),
		buildDNSLeaf,
		buildCLIParent,
	)
	if err != nil {
		panic(fmt.Sprintf("catalog compile dns: %v", err))
	}

	return &cli.Command{
		Name:        root.Name,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands:    cmds,
	}
}

// buildDNSLeaf is the mount-owned leaf builder materializing one DNS leaf into
// an urfave *cli.Command. Shape (name/category/aliases/flags/usage) comes from
// the clicatalog model via NewCLILeaf; behavior (the catalog action adapter +
// relaxFlagRequired) stays mount-owned here.
func buildDNSLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// dnsCatalogConfig returns the CatalogAdapterConfig that expresses dns' exact
// per-invocation behavior on top of the shared catalogActionAdapter pipeline.
// DNS needs four hooks: the result renderer, the per-invocation --auth-token
// override, the positional <domain>/<zone-id> mapping onto the "zone" input
// (unconditional overwrite, surplus args silently ignored), and the destructive
// --force gate for zone/record delete (refuse only when a zone target is
// present, message "dns <leaf>: ..."). The records-update "disabled" drop lands
// in MutateInput. All remaining fields stay nil so the shared pipeline's safe
// defaults apply.
//
// The leaf (and thus group/leaf-derived inputs the old adapter took from its
// `c`, `group`, `leaf` parameters) is reachable per-invocation via ic.Op (op
// leaf = last underscore segment of the canonical name) and ic.C (the mounted
// command), so this config needs no arguments — see describe comment below.
func dnsCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderDNSResult,
		HonorAuthTokenOverride: true,

		// Map the positional <domain>/<zone-id> into the "zone" input. The zone
		// arg is PositionalOnly on the DNS ops (no --zone flag), so the <domain>
		// positional is the only way to supply it. Surplus args are silently
		// ignored (the stricter MapPositionalArgs double-supply rejection is not
		// applied here).
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			if ic.C.Args().Len() > 0 {
				ic.Input["zone"] = ic.C.Args().First()
			}
			return nil
		},

		// dns_records_update: disabled is an omitempty field on the wire, and
		// omitting it must leave the record's current disabled state unchanged.
		// flagValue returns false for an unset bool, so drop the key entirely
		// when the flag was not given; the handler then leaves it nil (unchanged)
		// instead of forcing re-enable.
		MutateInput: func(ic *CatalogInvokeContext) error {
			if ic.Op.Name() == "dns_records_update" && !ic.C.IsSet(FlagDisabled) {
				delete(ic.Input, "disabled")
			}
			return nil
		},

		// Destructive gate (zones delete, records delete). The shared pipeline
		// enforces --force, honoring both --force and the hidden --confirm alias.
		// With a target zone and no --force, refuse loudly (non-zero exit) with
		// the "dns <leaf>:" message; with no zone, fall through so the handler's
		// required-argument validation produces a non-zero exit.
		DestructiveGate: GateForceReject(
			func(ic *CatalogInvokeContext) bool {
				return opmesh.StrArg(ic.Input, "zone", "") != ""
			},
			func(ic *CatalogInvokeContext) string {
				return "dns " + opLeafName(ic.Op) + ": pass --force to confirm this destructive operation"
			},
		),
	}
}

// renderDNSResult is the catalog.RenderFunc that renders a DNS handler's typed
// data result through the CLI Output formatter. It is the single rendering
// home for catalog-driven DNS commands and never touches core services.
func renderDNSResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
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
