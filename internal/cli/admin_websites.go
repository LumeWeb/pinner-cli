package cli

import "github.com/urfave/cli/v3"

// newAdminWebsitesCommand returns the admin `websites` section compiled from
// the catalog via adminSectionByName. The CLI command tree and the MCP tool
// surface share one source of truth in the catalog.
func newAdminWebsitesCommand() *cli.Command {
	return adminSectionByName(CmdWebsites)
}
