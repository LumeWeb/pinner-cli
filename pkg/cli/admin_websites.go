package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newAdminWebsitesCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdWebsites,
		Usage: "Manage IPFS websites (admin)",
		Description: `Administrative operations for IPFS websites.

Examples:
  pinner admin websites block <website-id>
  pinner admin websites unblock <website-id>`,
		Commands: []*cli.Command{
			newAdminWebsitesBlockCommand(),
			newAdminWebsitesUnblockCommand(),
		},
	}
}

func newAdminWebsitesBlockCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdBlock,
		Usage: "Block a website",
		Description: `Block a website by its ID.

Examples:
  pinner admin websites block <website-id>
  pinner admin websites block <website-id> --json`,
		ArgsUsage: "<website-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminWebsitesBlockAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultWebsiteAdminServiceFactory)
		},
	}
}

type adminWebsitesBlockCmdGetter interface {
	Args() cli.Args
}

func adminWebsitesBlockAction(ctx context.Context, cmd adminWebsitesBlockCmdGetter, output Output, cfgMgr config.Manager, serviceFactory WebsiteAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("website ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID := cmd.Args().First()
	website, err := service.BlockWebsite(ctx, websiteID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printfln("Website %s blocked successfully", websiteID)
	return nil
}

func newAdminWebsitesUnblockCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdUnblock,
		Usage: "Unblock a website",
		Description: `Unblock a previously blocked website by its ID.

Examples:
  pinner admin websites unblock <website-id>
  pinner admin websites unblock <website-id> --json`,
		ArgsUsage: "<website-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminWebsitesUnblockAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultWebsiteAdminServiceFactory)
		},
	}
}

type adminWebsitesUnblockCmdGetter interface {
	Args() cli.Args
}

func adminWebsitesUnblockAction(ctx context.Context, cmd adminWebsitesUnblockCmdGetter, output Output, cfgMgr config.Manager, serviceFactory WebsiteAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("website ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID := cmd.Args().First()
	website, err := service.UnblockWebsite(ctx, websiteID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printfln("Website %s unblocked successfully", websiteID)
	return nil
}
