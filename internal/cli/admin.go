package cli

import (
	"github.com/urfave/cli/v3"
)

func newAdminCommand() *cli.Command {
	// The admin parent is catalog-driven: the quota, billing, websites,
	// platform-domains and social-providers sections are compiled from the
	// canonical operation catalog in one CompileCommandTree pass (see
	// newAdminCatalogSections in catalog_admin_wiring.go and the shape model in
	// internal/clicatalog/shapes_admin.go). The `pprof` section is hand-written
	// (admin_pprof.go) and merged in because it has no catalog operation
	// backing.
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
		Commands: append(newAdminCatalogSections(), newAdminPprofCommand()),
	}
}

// newQuotaCommand returns the admin `quota` section compiled from the catalog.
// It is a thin, single-seam getter over adminSectionByName; the CLI command
// tree and the MCP tool surface share one source of truth via the catalog.
func newQuotaCommand() *cli.Command {
	return adminSectionByName(CmdQuota)
}

// newBillingCommand returns the admin `billing` section compiled from the
// catalog. It is a thin, single-seam getter over adminSectionByName.
func newBillingCommand() *cli.Command {
	return adminSectionByName(CmdBilling)
}
