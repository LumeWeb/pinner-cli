package cli

import "github.com/urfave/cli/v3"

// newAdminPlatformDomainsCommand returns the admin platform-domains command.
// It is compiled from the operation catalog in catalog_admin_wiring.go, so the
// CLI command tree and the MCP tool surface share one source of truth.
func newAdminPlatformDomainsCommand() *cli.Command {
	return newAdminPlatformDomainsCatalogCommand()
}
