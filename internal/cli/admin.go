package cli

import (
	"github.com/urfave/cli/v3"
)

func newAdminCommand() *cli.Command {
	return &cli.Command{
		Name:     "admin",
		Category: "Admin",
		Usage:    "Administrative operations",
		Description: `Administrative operations for quota management, billing, and profiling.

These commands require administrative privileges and are intended for system administrators.

Quota operations include:
  - List, create, update, delete quota plans
  - Manage user quota allowances
  - View system statistics
  - Reconcile quotas and cleanup expired data
  - Manage user quota configurations

Billing operations include:
  - Manage billing credits
  - View user balances
  - Manage price lines and pricing plans
  - Manage subscribers and subscriptions

Profiling operations include:
  - Access Go runtime pprof profiles (heap, cpu, goroutine, etc.)
  - Configure block and mutex profiling rates
  - View profiling status

Social provider operations include:
  - List, create, update, delete social login providers
  - Enable/disable providers for login

Examples:
  pinner admin quota plans list
  pinner admin quota allowances list
  pinner admin billing credits list
  pinner admin billing subscribers list
  pinner admin pprof status
  pinner admin pprof heap > heap.prof`,
		Commands: []*cli.Command{
			newQuotaCommand(),
			newBillingCommand(),
			newAdminWebsitesCommand(),
			newAdminPprofCommand(),
			newAdminPlatformDomainsCommand(),
			newAdminSocialProvidersCommand(),
		},
	}
}

// newQuotaCommand returns the admin quota command. It is compiled from the
// operation catalog in catalog_admin_wiring.go, so the CLI command tree and the
// MCP tool surface share one source of truth.
func newQuotaCommand() *cli.Command {
	return newAdminQuotaCatalogCommand()
}

// newBillingCommand returns the admin billing command. It is compiled from the
// operation catalog in catalog_admin_wiring.go, so the CLI command tree and the
// MCP tool surface share one source of truth.
func newBillingCommand() *cli.Command {
	return newAdminBillingCatalogCommand()
}
