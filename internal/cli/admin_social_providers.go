package cli

import "github.com/urfave/cli/v3"

// newAdminSocialProvidersCommand returns the admin social-providers command.
// It is compiled from the operation catalog in catalog_admin_wiring.go, so the
// CLI command tree and the MCP tool surface share one source of truth.
func newAdminSocialProvidersCommand() *cli.Command {
	return newAdminSocialProvidersCatalogCommand()
}
