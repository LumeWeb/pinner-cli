package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	coreadmin "go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// catalog_admin_wiring.go adapts the admin domain operations in
// internal/catalogops to the urfave CLI: it compiles catalog operations into
// commands under the "admin" parent, renders each handler's result through the
// Output formatter, and maps positionals and the destructive --force gate onto
// operation inputs. IO and CLI concerns live here, not in catalogops.
//
// Admin sections are added one at a time. This file currently mounts the
// platform-domains section (admin_platform_domains_*) under `admin
// platform-domains`; later sections (websites, quota, billing) add their own
// parent commands alongside it.

// catalogAdminDeps builds the catalogops.AdminDeps from the live CLI wiring.
// Services resolve lazily per invocation via the core factories; config is read
// at request time.
func catalogAdminDeps() catalogops.AdminDeps {
	return catalogops.AdminDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		PlatformDomainAdminService: func(cfgMgr config.Manager) (coreadmin.PlatformDomainAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultPlatformDomainAdminServiceFactory(cfgMgr), nil
		},
		WebsiteAdminService: func(cfgMgr config.Manager) (coreadmin.WebsiteAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultWebsiteAdminServiceFactory(cfgMgr), nil
		},
	}
}

// adminCatalogDepsVar is an indirection so the wiring and the renderer can both
// reach the canonical operation list without rebuilding it repeatedly.
var adminCatalogDepsVar = catalogops.AdminDeps(catalogAdminDeps())

// newAdminPlatformDomainsCatalogCommand compiles the admin platform-domains
// catalog operations and returns the `platform-domains` command to mount under
// the `admin` parent: list, register, update, delete, bind.
func newAdminPlatformDomainsCatalogCommand() *cli.Command {
	return newAdminSectionCommand("admin_platform_domains_", CmdPlatformDomains, "Manage platform (free-subdomain) root domains")
}

// newAdminWebsitesCatalogCommand compiles the admin websites catalog operations
// into a `websites` command for the `admin` parent: block, unblock.
func newAdminWebsitesCatalogCommand() *cli.Command {
	return newAdminSectionCommand("admin_websites_", CmdWebsites, "Manage IPFS websites (admin)")
}

// newAdminSectionCommand compiles the admin catalog operations whose names start
// with prefix into a single parent command under `admin`, stripping prefix from
// each leaf name. Leaves have their flag-required markers relaxed so positionals
// can supply required args, and their Action wrapped with the CLI adapter.
func newAdminSectionCommand(prefix, parentName, usage string) *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.AdminOperations(adminCatalogDepsVar) {
		if !strings.HasPrefix(op.Name(), prefix) {
			continue
		}
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile admin section %q: %v", parentName, err))
	}

	parent := &cli.Command{
		Name:     parentName,
		Category: "Admin",
		Usage:    usage,
		Commands: []*cli.Command{},
	}
	for _, c := range compiled {
		parent.Commands = append(parent.Commands, mountAdminSectionCommand(c, prefix))
	}
	return parent
}

// mountAdminSectionCommand adapts a single catalog-compiled command into a live
// admin subcommand: strips the section prefix, relaxes flag-required markers so
// positionals supply required args, and wraps the Action with the CLI adapter.
func mountAdminSectionCommand(cmd *cli.Command, prefix string) *cli.Command {
	canonical := cmd.Name
	display := strings.TrimPrefix(canonical, prefix)
	cmd.Name = display
	cmd.Category = "Admin"

	relaxFlagRequired(cmd)

	var op catalog.Operation
	for _, cand := range catalogops.AdminOperations(adminCatalogDepsVar) {
		if cand.Name() == canonical {
			op = cand
			break
		}
	}
	if op != nil {
		cmd.Action = adminActionAdapter(op)
	}
	return cmd
}

// adminActionAdapter returns the per-invocation ActionFunc for an admin catalog
// operation. It builds the input map from flags plus resolved positionals,
// threads the --auth-token override, applies the destructive --force gate, and
// renders the handler result.
func adminActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		input := catalog.FlagsToInput(c, op)
		// Note: no --auth-token override is threaded here. Admin services read
		// auth from the live config manager's token, so a per-invocation flag
		// override is not currently supported for the admin domain.
		if err := applyPositionalArgs(op, input, c.Args()); err != nil {
			return err
		}

		// Destructive gate: the delete op requires confirm=true. Map --force (or
		// --confirm) onto the op's confirm input before execution.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			input["confirm"] = confirm
			if !confirm {
				return fmt.Errorf("admin platform-domains delete: pass --force to confirm this destructive operation")
			}
		}

		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}
		return renderAdminResult(ctx, c, op, result)
	}
}

// renderAdminResult renders an admin handler's typed result through the CLI
// Output formatter.
func renderAdminResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)
	if result != nil && isNilPointerResult(result) {
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case *catalogops.AdminPlatformDomainsListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "platform_domains": r.PlatformDomains})
		}
		output.Printfln("Found %d platform domain(s)", r.Count)
		if len(r.PlatformDomains) == 0 {
			return nil
		}
		headers := []string{"ID", "DOMAIN", "NAMESPACE", "ZONE", "ENABLED"}
		rows := make([][]string, len(r.PlatformDomains))
		for i, d := range r.PlatformDomains {
			rows[i] = []string{
				fmt.Sprintf("%d", d.Id), d.Domain, d.Namespace,
				fmt.Sprintf("%d", d.ZoneId), yesNo(d.Enabled),
			}
		}
		output.PrintTable(headers, rows)
		return nil

	case *admin.PlatformDomain:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Platform domain %s (ID %d)", r.Domain, r.Id)
		return nil

	case *admin.Website:
		// admin websites block/unblock return a Website (embedded response).
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Website %s (ID %d): %s", r.Domain, r.Id, r.Status)
		return nil

	case *admin.RootDomain:
		// admin platform-domains bind returns a RootDomain (the bound apex).
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Bound website to platform domain %s (domain ID %d)", r.Domain, r.Id)
		return nil

	case *catalogops.AdminPlatformDomainsDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "id": r.ID})
		}
		output.Printfln("Platform domain %s deleted", r.ID)
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// yesNo renders a bool as "yes"/"no" for tables.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
