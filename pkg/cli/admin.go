package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newAdminCommand() *cli.Command {
	return &cli.Command{
		Name:  "admin",
		Usage: "Administrative operations",
		Description: `Administrative operations for quota management and billing.

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

Examples:
  pinner admin quota plans list
  pinner admin quota allowances list
  pinner admin billing credits list
  pinner admin billing subscribers list`,
		Commands: []*cli.Command{
			newQuotaCommand(),
			newBillingCommand(),
			newAdminWebsitesCommand(),
		},
	}
}

func newQuotaCommand() *cli.Command {
	return &cli.Command{
		Name:  "quota",
		Usage: "Quota management operations",
		Description: `Manage quota plans, allowances, and user configurations.

Quota operations include:
  - Plan management (list, create, update, delete)
  - Allowance management (list, create, update, delete)
  - User config management
  - System statistics and reconciliation

Examples:
  pinner admin quota plans list
  pinner admin quota plans get <plan-id>
  pinner admin quota allowances list
  pinner admin quota stats`,
		Commands: []*cli.Command{
			newQuotaPlansCommand(),
			newQuotaAllowancesCommand(),
			newQuotaUserConfigsCommand(),
			newQuotaStatsCommand(),
			newQuotaReconcileCommand(),
			newQuotaCleanupCommand(),
		},
	}
}

func newBillingCommand() *cli.Command {
	return &cli.Command{
		Name:  "billing",
		Usage: "Billing management operations",
		Description: `Manage billing credits, price lines, pricing plans, and subscriptions.

Billing operations include:
  - Credit management (list, create, delete, restore, purge)
  - User balance viewing
  - Price line management
  - Pricing plan and period management
  - Subscriber and subscription management

Examples:
  pinner admin billing credits list
  pinner admin billing price-lines list
  pinner admin billing pricing-plans list
  pinner admin billing subscribers list`,
		Commands: []*cli.Command{
			newBillingOverviewCommand(),
			newBillingCreditsCommand(),
			newBillingPriceLinesCommand(),
			newBillingPricingPlansCommand(),
			newBillingPricingPlanPeriodsCommand(),
			newBillingSubscribersCommand(),
		},
	}
}

func newQuotaPlansCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPlans,
		Usage: "Manage quota plans",
		Description: `List, create, update, and delete quota plans.

Examples:
  pinner admin quota plans list
  pinner admin quota plans get <plan-id>`,
		Commands: []*cli.Command{
			{
				Name:  CmdList,
				Usage: "List all quota plans",
				Description: `List all available quota plans.

Examples:
  pinner admin quota plans list
  pinner admin quota plans list --json`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdGet,
				Usage: "Get a quota plan by ID",
				Description: `Get details of a specific quota plan.

Examples:
  pinner admin quota plans get <plan-id>
  pinner admin quota plans get <plan-id> --json`,
				ArgsUsage: "<plan-id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdCreate,
				Usage: "Create a new quota plan",
				Description: `Create a new quota plan with specified limits.

Examples:
  pinner admin quota plans create --name "Pro" --upload 1000 --download 2000 --storage 5000
  pinner admin quota plans create --name "Free" --is-active --is-default
  pinner admin quota plans create --name "Basic" --description "Basic tier" --is-active`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  FlagName,
						Usage: "Plan name",
					},
					&cli.StringFlag{
						Name:  FlagDescription,
						Usage: "Plan description",
					},
					&cli.IntFlag{
						Name:  FlagUploadLimit,
						Usage: "Upload limit (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadLimit,
						Usage: "Download limit (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageLimit,
						Usage: "Storage limit (bytes)",
					},
						&cli.StringFlag{
							Name:  FlagWindowType,
							Usage: "Window type (ROLLING, DAY, WEEK, MONTH, YEAR, LIFETIME)",
						},
					&cli.BoolFlag{
						Name:  FlagIsActive,
						Usage: "Mark plan as active",
						Value: false,
					},
					&cli.BoolFlag{
						Name:  FlagIsDefault,
						Usage: "Set as default plan for new users",
						Value: false,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdUpdate,
				Usage: "Update a quota plan",
				Description: `Update an existing quota plan.

Examples:
  pinner admin quota plans update <plan-id> --name "Updated Pro"
  pinner admin quota plans update <plan-id> --is-active --is-default`,
				ArgsUsage: "<plan-id>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  FlagName,
						Usage: "Plan name",
					},
					&cli.StringFlag{
						Name:  FlagDescription,
						Usage: "Plan description",
					},
					&cli.IntFlag{
						Name:  FlagUploadLimit,
						Usage: "Upload limit (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadLimit,
						Usage: "Download limit (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageLimit,
						Usage: "Storage limit (bytes)",
					},
						&cli.StringFlag{
							Name:  FlagWindowType,
							Usage: "Window type (ROLLING, DAY, WEEK, MONTH, YEAR, LIFETIME)",
						},
					&cli.BoolFlag{
						Name:  FlagIsActive,
						Usage: "Mark plan as active",
					},
					&cli.BoolFlag{
						Name:  FlagIsDefault,
						Usage: "Set as default plan for new users",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdDelete,
				Usage: "Delete a quota plan",
				Description: `Delete a quota plan by ID.

Examples:
  pinner admin quota plans delete <plan-id>`,
				ArgsUsage: "<plan-id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdSetDefault,
				Usage: "Set a quota plan as default",
				Description: `Set a quota plan as the default for new users.

Examples:
  pinner admin quota plans set-default <plan-id>`,
				ArgsUsage: "<plan-id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaPlansSetDefaultAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
		},
	}
}

func newQuotaAllowancesCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdAllowances,
		Usage: "Manage quota allowances",
		Description: `List, create, update, and delete quota allowances.

Examples:
  pinner admin quota allowances list
  pinner admin quota allowances create --user-id 123 --type bonus`,
		Commands: []*cli.Command{
			{
				Name:  CmdList,
				Usage: "List all quota allowances",
				Description: `List all quota allowances.

Examples:
  pinner admin quota allowances list
  pinner admin quota allowances list --json`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaAllowancesListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdCreate,
				Usage: "Create a quota allowance",
				Description: `Create a new quota allowance for a user.

Examples:
  pinner admin quota allowances create --user-id 123 --source admin --type bonus --upload 1000`,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  FlagUserID,
						Usage: "User ID",
					},
					&cli.StringFlag{
						Name:  FlagSource,
						Usage: "Allowance source",
					},
					&cli.StringFlag{
						Name:  FlagQuotaType,
						Usage: "Allowance type",
					},
					&cli.IntFlag{
						Name:  FlagUploadLimit,
						Usage: "Upload allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadLimit,
						Usage: "Download allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageLimit,
						Usage: "Storage allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagExpiry,
						Usage: "Expiry in days from now",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaAllowancesCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdUpdate,
				Usage: "Update a quota allowance",
				Description: `Update an existing quota allowance.

Examples:
  pinner admin quota allowances update <grant-id> --upload-limit 2000`,
				ArgsUsage: "<grant-id>",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  FlagUserID,
						Usage: "User ID",
					},
					&cli.StringFlag{
						Name:  FlagSource,
						Usage: "Allowance source",
					},
					&cli.StringFlag{
						Name:  FlagQuotaType,
						Usage: "Allowance type",
					},
					&cli.IntFlag{
						Name:  FlagUploadLimit,
						Usage: "Upload allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadLimit,
						Usage: "Download allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageLimit,
						Usage: "Storage allowance (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagExpiry,
						Usage: "Expiry in days from now",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaAllowancesUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdDelete,
				Usage: "Delete a quota allowance",
				Description: `Delete a quota allowance by grant ID.

Examples:
  pinner admin quota allowances delete <grant-id>`,
				ArgsUsage: "<grant-id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaAllowancesDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
		},
	}
}

func newQuotaUserConfigsCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdUserConfigs,
		Usage: "Manage user quota configurations",
		Description: `List and update user quota configurations.

Examples:
  pinner admin quota user-configs list
  pinner admin quota user-configs update --user-id 1 --plan-id 19
  pinner admin quota user-configs reset <user-id>`,
		Commands: []*cli.Command{
			{
				Name:  CmdList,
				Usage: "List all user quota configs",
				Description: `List all user quota configurations.

Examples:
  pinner admin quota user-configs list
  pinner admin quota user-configs list --json`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaUserConfigsListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdUpdate,
				Usage: "Update a user quota config",
				Description: `Update a user's quota configuration.

Examples:
  pinner admin quota user-configs update --user-id 1 --plan-id 19
  pinner admin quota user-configs update --user-id 1 --plan-id 19 --enforcement-policy strict
  pinner admin quota user-configs update --user-id 1 --upload-limit-bytes 5000 --download-limit-bytes 10000
  pinner admin quota user-configs update --user-id 1 --plan-id 19 --json`,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  FlagUserID,
						Usage: "User ID",
					},
					&cli.IntFlag{
						Name:  FlagPlanID,
						Usage: "Quota plan ID to assign",
					},
					&cli.StringFlag{
						Name:  FlagEnforcementPolicy,
						Usage: "Enforcement policy (e.g. strict, relaxed)",
					},
					&cli.IntFlag{
						Name:  FlagUploadLimit,
						Usage: "Upload limit override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadLimit,
						Usage: "Download limit override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageLimit,
						Usage: "Storage limit override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagUploadThreshold,
						Usage: "Upload threshold override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagDownloadThreshold,
						Usage: "Download threshold override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagStorageThreshold,
						Usage: "Storage threshold override (bytes)",
					},
					&cli.IntFlag{
						Name:  FlagWindowDuration,
						Usage: "Window duration override",
					},
					&cli.IntFlag{
						Name:  FlagWindowStartHour,
						Usage: "Window start hour override",
					},
					&cli.StringFlag{
						Name:  FlagWindowTimezone,
						Usage: "Window timezone override",
					},
					&cli.StringFlag{
						Name:  FlagWindowType,
						Usage: "Window type override (ROLLING, DAY, WEEK, MONTH, YEAR, LIFETIME)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaUserConfigsUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
			{
				Name:  CmdReset,
				Usage: "Reset user plan to default",
				Description: `Reset a user's quota plan to the default.

Examples:
  pinner admin quota user-configs reset <user-id>`,
				ArgsUsage: "<user-id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgMgr, output, err := setupCommandContext(cmd)
					if err != nil {
						return err
					}
					return quotaUserConfigsResetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
				},
			},
		},
	}
}

func newQuotaStatsCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdStats,
		Usage: "Get quota system statistics",
		Description: `View quota system statistics.

Examples:
  pinner admin quota stats
  pinner admin quota stats --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return quotaStatsAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
		},
	}
}

func newQuotaReconcileCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdReconcile,
		Usage: "Reconcile quota data",
		Description: `Reconcile quota data for all users or a specific user.

Examples:
  pinner admin quota reconcile
  pinner admin quota reconcile --user-id 123`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "user-id",
				Usage: "Specific user ID to reconcile (optional)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return quotaReconcileAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
		},
	}
}

func newQuotaCleanupCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdCleanup,
		Usage: "Cleanup expired quota data",
		Description: `Cleanup expired quota data older than the specified retention period.

Examples:
  pinner admin quota cleanup --retention-days 90`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "retention-days",
				Usage: "Retention period in days",
				Value: 90,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return quotaCleanupAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultQuotaAdminServiceFactory)
		},
	}
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
