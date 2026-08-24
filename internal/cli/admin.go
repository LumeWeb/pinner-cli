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

func newBillingPricingPlansCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPricingPlans,
		Usage: "Manage billing pricing plans",
		Description: `List, create, update, delete, and sync billing pricing plans.

Examples:
  pinner admin billing pricing-plans list
  pinner admin billing pricing-plans sync <plan-id>
  pinner admin billing pricing-plans sync-all`,
		Commands: []*cli.Command{
			newBillingPricingPlansListCommand(),
			newBillingPricingPlansGetCommand(),
			newBillingPricingPlansCreateCommand(),
			newBillingPricingPlansUpdateCommand(),
			newBillingPricingPlansDeleteCommand(),
			newBillingSyncCommand(),
			newBillingSyncAllCommand(),
		},
	}
}

func newBillingPricingPlanPeriodsCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPricingPlanPeriods,
		Usage: "Manage billing pricing plan periods",
		Description: `List, create, update, and delete billing pricing plan periods.

  pinner admin billing pricing-plan-periods list`,
		Commands: []*cli.Command{
			newBillingPricingPlanPeriodsListCommand(),
			newBillingPricingPlanPeriodsGetCommand(),
			newBillingPricingPlanPeriodsCreateCommand(),
			newBillingPricingPlanPeriodsUpdateCommand(),
			newBillingPricingPlanPeriodsDeleteCommand(),
		},
	}
}

func newBillingSubscribersCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSubscribers,
		Usage: "Manage billing subscribers",
		Description: `List, manage, and modify billing subscribers.

Examples:
  pinner admin billing subscribers list
  pinner admin billing subscribers get <id>
  pinner admin billing subscribers list-gateway <gateway-id>
  pinner admin billing subscribers list-user <user-id>
  pinner admin billing subscribers cancel --user-id 123
  pinner admin billing subscribers abort-cancel --user-id 123
  pinner admin billing subscribers change-plan --user-id 123 --plan-id "plan-abc"
  pinner admin billing subscribers pause --user-id 123
  pinner admin billing subscribers resume --user-id 123`,
		Commands: []*cli.Command{
			newBillingSubscribersListCommand(),
			newBillingSubscribersGetCommand(),
			newBillingSubscribersListGatewayCommand(),
			newBillingSubscribersListUserCommand(),
			newBillingSubscribersCancelCommand(),
			newBillingSubscribersAbortCancelCommand(),
			newBillingSubscribersChangePlanCommand(),
			newBillingSubscribersPauseCommand(),
			newBillingSubscribersResumeCommand(),
		},
	}
}
