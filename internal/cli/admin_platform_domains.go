package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// newAdminPlatformDomainsCommand returns the platform-domains admin
// subcommand tree. Platform domains are platform-owned root domains that users
// can claim free subdomains under.
func newAdminPlatformDomainsCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPlatformDomains,
		Usage: "Manage platform (free-subdomain) root domains",
		Description: `Manage platform-owned root domains that users can claim free subdomains under.

Examples:
  pinner admin platform-domains list
  pinner admin platform-domains register --domain ipfs.pin.xyz --namespace icann
  pinner admin platform-domains update <id> --enabled false
  pinner admin platform-domains delete <id>
  pinner admin platform-domains bind <id> --website-id 42`,
		Commands: []*cli.Command{
			newAdminPlatformDomainsListCommand(),
			newAdminPlatformDomainsRegisterCommand(),
			newAdminPlatformDomainsUpdateCommand(),
			newAdminPlatformDomainsDeleteCommand(),
			newAdminPlatformDomainsBindCommand(),
		},
	}
}

func newAdminPlatformDomainsListCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdList,
		Usage: "List platform domains",
		Description: `List all registered platform-owned root domains, including disabled ones.

Examples:
  pinner admin platform-domains list
  pinner admin platform-domains list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPlatformDomainsListAction(ctx, output, cfgMgr, defaultPlatformDomainAdminServiceFactory)
		},
	}
}

func newAdminPlatformDomainsRegisterCommand() *cli.Command {
	return &cli.Command{
		Name:  "register",
		Usage: "Register a platform domain",
		Description: `Register a platform-owned root domain that users can claim free subdomains under.

Examples:
  pinner admin platform-domains register --domain ipfs.pin.xyz --namespace icann
  pinner admin platform-domains register --domain hns.pin.xyz --namespace hns --enabled
  pinner admin platform-domains register --domain ipfs.pin.xyz --namespace icann --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagDomain, Usage: "Platform root domain, e.g. ipfs.pin.xyz"},
			&cli.StringFlag{Name: FlagNamespace, Usage: "Domain namespace: icann, hns, etc."},
			&cli.BoolFlag{Name: FlagEnabled, Usage: "Enable the platform domain so users can claim subdomains under it"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPlatformDomainsRegisterAction(ctx, cmd, output, cfgMgr, defaultPlatformDomainAdminServiceFactory)
		},
	}
}

func newAdminPlatformDomainsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdUpdate,
		Usage: "Update a platform domain",
		Description: `Enable or disable a registered platform root. Disabling prevents new claims but does not delete existing bindings.

Examples:
  pinner admin platform-domains update <id> --enabled false
  pinner admin platform-domains update <id> --enabled true --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: FlagEnabled, Usage: "Enable (true) or disable (false) the platform domain"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPlatformDomainsUpdateAction(ctx, cmd, output, cfgMgr, defaultPlatformDomainAdminServiceFactory)
		},
	}
}

func newAdminPlatformDomainsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdDelete,
		Usage: "Delete a platform domain",
		Description: `Remove a registered platform root. Existing subdomain bindings remain but can no longer be reconciled as platform subdomains.

Examples:
  pinner admin platform-domains delete <id>`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPlatformDomainsDeleteAction(ctx, cmd, output, cfgMgr, defaultPlatformDomainAdminServiceFactory)
		},
	}
}

func newAdminPlatformDomainsBindCommand() *cli.Command {
	return &cli.Command{
		Name:  "bind",
		Usage: "Bind a website to a platform domain",
		Description: `Bind an operator-owned website directly to the root apex of a platform domain (e.g. "pinner.site"). The platform root's DNS zone is auto-created on first use.

Examples:
  pinner admin platform-domains bind <id> --website-id 42
  pinner admin platform-domains bind <id> --website-id 42 --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: FlagWebsiteID, Usage: "ID of the operator-owned website to bind"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPlatformDomainsBindAction(ctx, cmd, output, cfgMgr, defaultPlatformDomainAdminServiceFactory)
		},
	}
}

func adminPlatformDomainsBindAction(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, serviceFactory PlatformDomainAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	if cmd.Args().Len() < 1 {
		return fmt.Errorf("platform domain ID is required")
	}

	websiteID := cmd.Int(FlagWebsiteID)
	if websiteID == 0 {
		return fmt.Errorf("--website-id is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	id := cmd.Args().First()
	req := &admin.PlatformDomainBindRequest{WebsiteId: websiteID}

	result, err := service.BindWebsiteToPlatformDomain(ctx, id, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}
	output.Printfln("Website %d bound to platform domain %s (domain ID %d)", websiteID, result.Domain, result.Id)
	return nil
}

func adminPlatformDomainsListAction(ctx context.Context, output Output, cfgMgr config.Manager, serviceFactory PlatformDomainAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	domains, total, err := service.ListPlatformDomains(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{"count": total, "platform_domains": domains})
	}

	output.Printfln("Found %d platform domain(s)", total)
	if len(domains) == 0 {
		return nil
	}

	headers := []string{"ID", "DOMAIN", "NAMESPACE", "ZONE", "ENABLED"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		rows[i] = []string{
			strconv.Itoa(d.Id),
			d.Domain,
			d.Namespace,
			strconv.Itoa(d.ZoneId),
			boolYesNo(d.Enabled),
		}
	}
	output.PrintTable(headers, rows)
	return nil
}

func adminPlatformDomainsRegisterAction(ctx context.Context, cmd flagGetterWithIsSet, output Output, cfgMgr config.Manager, serviceFactory PlatformDomainAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	domain := cmd.String(FlagDomain)
	if domain == "" {
		return fmt.Errorf("--domain is required")
	}
	namespace := cmd.String(FlagNamespace)
	if namespace == "" {
		return fmt.Errorf("--namespace is required")
	}

	req := &admin.PlatformDomainRequest{
		Domain:    domain,
		Namespace: namespace,
	}
	if cmd.IsSet(FlagEnabled) {
		enabled := cmd.Bool(FlagEnabled)
		req.Enabled = &enabled
	}

	result, err := service.RegisterPlatformDomain(ctx, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}
	output.Printfln("Platform domain %s registered (ID %d)", result.Domain, result.Id)
	return nil
}

func adminPlatformDomainsUpdateAction(ctx context.Context, cmd argsFlagGetterWithBool, output Output, cfgMgr config.Manager, serviceFactory PlatformDomainAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	if cmd.Args().Len() < 1 {
		return fmt.Errorf("platform domain ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	id := cmd.Args().First()
	req := &admin.PlatformDomainUpdateRequest{Enabled: cmd.Bool(FlagEnabled)}

	result, err := service.UpdatePlatformDomain(ctx, id, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}
	output.Printfln("Platform domain %s updated: enabled=%t", result.Domain, result.Enabled)
	return nil
}

func adminPlatformDomainsDeleteAction(ctx context.Context, cmd argsGetter, output Output, cfgMgr config.Manager, serviceFactory PlatformDomainAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	if cmd.Args().Len() < 1 {
		return fmt.Errorf("platform domain ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	id := cmd.Args().First()
	if err := service.DeletePlatformDomain(ctx, id); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{"deleted": true, "id": id})
	}
	output.Printfln("Platform domain %s deleted", id)
	return nil
}

func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
