package cli

import "github.com/urfave/cli/v3"

// newAdminPlatformDomainsCommand returns the admin `platform-domains` section
// compiled from the catalog via adminSectionByName. The CLI command tree and
// the MCP tool surface share one source of truth in the catalog.
func newAdminPlatformDomainsCommand() *cli.Command {
	return adminSectionByName(CmdPlatformDomains)
}
