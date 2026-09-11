package cli

import "github.com/urfave/cli/v3"

// newAdminSocialProvidersCommand returns the admin `social-providers` section
// compiled from the catalog via adminSectionByName. The CLI command tree and
// the MCP tool surface share one source of truth in the catalog.
func newAdminSocialProvidersCommand() *cli.Command {
	return adminSectionByName(CmdSocialProviders)
}
