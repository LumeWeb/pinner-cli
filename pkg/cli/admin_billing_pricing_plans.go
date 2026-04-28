package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

func newBillingPricingPlansListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all pricing plans",
		Description: `List all billing pricing plans.

Examples:
  pinner admin billing pricing-plans list
  pinner admin billing pricing-plans list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlansListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlansListCmdGetter is an empty interface for list command (no args/flags needed)
type billingPricingPlansListCmdGetter interface{}

func billingPricingPlansListAction(ctx context.Context, cmd billingPricingPlansListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	plans, total, err := service.ListPricingPlans(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"plans": plans,
			"total": total,
		})
	}

	output.Printfln("Total pricing plans: %d", total)
	if len(plans) > 0 {
		headers := []string{"ID", "Name", "Description", "Currency", "Active"}
		rows := make([][]string, len(plans))
		for i, p := range plans {
			desc := ""
			if p.Description != "" {
				desc = p.Description
			}
			rows[i] = []string{
				fmt.Sprintf("%d", p.Id),
				p.Name,
				desc,
				p.Currency,
				fmt.Sprintf("%t", p.IsActive),
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingPricingPlansCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a pricing plan",
		Description: `Create a new billing pricing plan.

Examples:
  pinner admin billing pricing-plans create --name "Pro Plan" --currency USD
  pinner admin billing pricing-plans create --name "Basic" --currency USD --description "Basic plan" --is-active
  pinner admin billing pricing-plans create --name "Premium" --currency USD --is-public --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "name",
				Usage:    "Pricing plan name",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "currency",
				Usage:    "Currency code (e.g., USD, EUR)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Plan description",
			},
			&cli.BoolFlag{
				Name:  "is-active",
				Usage: "Mark plan as active",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "is-public",
				Usage: "Mark plan as public",
				Value: false,
			},
			&cli.IntFlag{
				Name:  "priceline-id",
				Usage: "Associated price line ID",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlansCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlansCreateCmdGetter defines the interface for getting create command flags.
type billingPricingPlansCreateCmdGetter interface {
	String(name string) string
	Bool(name string) bool
	Int(name string) int
	IsSet(name string) bool
}

func billingPricingPlansCreateAction(ctx context.Context, cmd billingPricingPlansCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := admin.PricingPlanCreateRequest{
		Name:           cmd.String("name"),
		Currency:       cmd.String("currency"),
		IsActive:       cmd.Bool("is-active"),
		IsPublic:       cmd.Bool("is-public"),
		Description:    cmd.String("description"),
		PricingPeriods: []admin.PricingPlanPeriod{},
	}

	if cmd.IsSet("priceline-id") {
		pricelineId := cmd.Int("priceline-id")
		req.PricelineId = &pricelineId
	}

	plan, err := service.CreatePricingPlan(ctx, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(plan)
	}

	output.Printfln("Pricing plan created successfully:")
	output.Printfln("  ID: %d", plan.Id)
	output.Printfln("  Name: %s", plan.Name)
	output.Printfln("  Currency: %s", plan.Currency)
	output.Printfln("  Active: %t", plan.IsActive)
	return nil
}

func newBillingPricingPlansUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a pricing plan",
		Description: `Update an existing billing pricing plan.

Examples:
  pinner admin billing pricing-plans update <id> --name "Updated Pro"
  pinner admin billing pricing-plans update <id> --description "New desc" --is-active false --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "name",
				Usage: "Pricing plan name",
			},
			&cli.StringFlag{
				Name:  "currency",
				Usage: "Currency code (e.g., USD, EUR)",
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Plan description",
			},
			&cli.BoolFlag{
				Name:  "is-active",
				Usage: "Mark plan as active",
			},
			&cli.BoolFlag{
				Name:  "is-public",
				Usage: "Mark plan as public",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlansUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlansUpdateCmdGetter defines the interface for getting update command args and flags.
type billingPricingPlansUpdateCmdGetter interface {
	Args() cli.Args
	String(name string) string
	Bool(name string) bool
	IsSet(name string) bool
}

func billingPricingPlansUpdateAction(ctx context.Context, cmd billingPricingPlansUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("pricing plan ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	planID := cmd.Args().First()

	req := admin.PricingPlanUpdateRequest{}

	if cmd.IsSet("name") {
		req.Name = cmd.String("name")
	}
	if cmd.IsSet("currency") {
		req.Currency = cmd.String("currency")
	}
	if cmd.IsSet("description") {
		req.Description = cmd.String("description")
	}
	if cmd.IsSet("is-active") {
		req.IsActive = cmd.Bool("is-active")
	}
	if cmd.IsSet("is-public") {
		req.IsPublic = cmd.Bool("is-public")
	}

	plan, err := service.UpdatePricingPlan(ctx, planID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(plan)
	}

	output.Printfln("Pricing plan updated successfully:")
	output.Printfln("  ID: %d", plan.Id)
	output.Printfln("  Name: %s", plan.Name)
	output.Printfln("  Currency: %s", plan.Currency)
	output.Printfln("  Active: %t", plan.IsActive)
	return nil
}

func newBillingPricingPlansDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a pricing plan",
		Description: `Delete a pricing plan by ID.

Examples:
  pinner admin billing pricing-plans delete <id>
  pinner admin billing pricing-plans delete <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlansDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlansDeleteCmdGetter defines the interface for getting delete command args.
type billingPricingPlansDeleteCmdGetter interface {
	Args() cli.Args
}

func billingPricingPlansDeleteAction(ctx context.Context, cmd billingPricingPlansDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("pricing plan ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	planID := cmd.Args().First()
	if err := service.DeletePricingPlan(ctx, planID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":  "deleted",
			"plan_id": planID,
		})
	}

	output.Printfln("Pricing plan %s deleted successfully", planID)
	return nil
}

func newBillingPricingPlanPeriodsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all pricing plan periods",
		Description: `List all billing pricing plan periods.

Examples:
  pinner admin billing pricing-plan-periods list
  pinner admin billing pricing-plan-periods list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlanPeriodsListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlanPeriodsListCmdGetter is an empty interface for list command (no args/flags needed)
type billingPricingPlanPeriodsListCmdGetter interface{}

func billingPricingPlanPeriodsListAction(ctx context.Context, cmd billingPricingPlanPeriodsListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	periods, total, err := service.ListPricingPlanPeriods(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"periods": periods,
			"total":   total,
		})
	}

	output.Printfln("Total pricing plan periods: %d", total)
	if len(periods) > 0 {
		headers := []string{"ID", "Plan ID", "Price (USD)", "Cadence", "Active"}
		rows := make([][]string, len(periods))
		for i, p := range periods {
			rows[i] = []string{
				fmt.Sprintf("%d", p.Id),
				fmt.Sprintf("%d", p.PricingPlanId),
				fmt.Sprintf("%.2f", p.PriceUsd),
				p.Cadence,
				fmt.Sprintf("%t", p.IsActive),
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingPricingPlanPeriodsGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get pricing plan period by ID",
		Description: `Get details of a specific pricing plan period.

Examples:
  pinner admin billing pricing-plan-periods get <id>
  pinner admin billing pricing-plan-periods get <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlanPeriodsGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlanPeriodsGetCmdGetter defines the interface for getting get command args.
type billingPricingPlanPeriodsGetCmdGetter interface {
	Args() cli.Args
}

func billingPricingPlanPeriodsGetAction(ctx context.Context, cmd billingPricingPlanPeriodsGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("period ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	periodID := cmd.Args().First()
	period, err := service.GetPricingPlanPeriod(ctx, periodID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(period)
	}

	output.Printfln("Period ID: %d", period.Id)
	output.Printfln("Plan ID: %d", period.PricingPlanId)
	output.Printfln("Price (USD): %.2f", period.PriceUsd)
	output.Printfln("Cadence: %s", period.Cadence)
	output.Printfln("Active: %t", period.IsActive)
	output.Printfln("Quota Plan ID: %d", period.QuotaPlanId)
	if period.RollingDays != nil {
		output.Printfln("Rolling Days: %d", *period.RollingDays)
	}
	output.Printfln("Created At: %s", period.CreatedAt.Format("2006-01-02 15:04:05"))
	output.Printfln("Updated At: %s", period.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}

func newBillingPricingPlanPeriodsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a pricing plan period",
		Description: `Create a new billing pricing plan period.

Examples:
  pinner admin billing pricing-plan-periods create --plan-id 123 --price 9.99 --cadence monthly --quota-plan-id 1
  pinner admin billing pricing-plan-periods create --plan-id 123 --price 99.99 --cadence yearly --quota-plan-id 1 --json`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "plan-id",
				Usage:    "Pricing plan ID",
				Required: true,
			},
			&cli.FloatFlag{
				Name:     "price",
				Usage:    "Price in USD",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "cadence",
				Usage:    "Cadence (e.g., monthly, yearly)",
				Required: true,
			},
			&cli.IntFlag{
				Name:     "quota-plan-id",
				Usage:    "Associated quota plan ID",
				Required: true,
			},
			&cli.IntFlag{
				Name:  "rolling-days",
				Usage: "Rolling days (for rolling periods)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlanPeriodsCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlanPeriodsCreateCmdGetter defines the interface for getting create command flags.
type billingPricingPlanPeriodsCreateCmdGetter interface {
	Int(name string) int
	Float(name string) float64
	String(name string) string
	IsSet(name string) bool
}

func billingPricingPlanPeriodsCreateAction(ctx context.Context, cmd billingPricingPlanPeriodsCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := admin.PricingPlanPeriodCreateRequest{
		PricingPlanId: cmd.Int("plan-id"),
		PriceUsd:      float32(cmd.Float("price")),
		Cadence:       cmd.String("cadence"),
		QuotaPlanId:   cmd.Int("quota-plan-id"),
	}

	if cmd.IsSet("rolling-days") {
		rollingDays := cmd.Int("rolling-days")
		req.RollingDays = &rollingDays
	}

	period, err := service.CreatePricingPlanPeriod(ctx, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(period)
	}

	output.Printfln("Pricing plan period created successfully:")
	output.Printfln("  ID: %d", period.Id)
	output.Printfln("  Plan ID: %d", period.PricingPlanId)
	output.Printfln("  Price: %.2f USD", period.PriceUsd)
	output.Printfln("  Cadence: %s", period.Cadence)
	return nil
}

func newBillingPricingPlanPeriodsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a pricing plan period",
		Description: `Update an existing billing pricing plan period.

Examples:
  pinner admin billing pricing-plan-periods update <id> --price 19.99
  pinner admin billing pricing-plan-periods update <id> --cadence yearly --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.FloatFlag{
				Name:  "price",
				Usage: "Price in USD",
			},
			&cli.StringFlag{
				Name:  "cadence",
				Usage: "Cadence (e.g., monthly, yearly)",
			},
			&cli.IntFlag{
				Name:  "quota-plan-id",
				Usage: "Associated quota plan ID",
			},
			&cli.IntFlag{
				Name:  "rolling-days",
				Usage: "Rolling days (for rolling periods)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlanPeriodsUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlanPeriodsUpdateCmdGetter defines the interface for getting update command args and flags.
type billingPricingPlanPeriodsUpdateCmdGetter interface {
	Args() cli.Args
	Float(name string) float64
	String(name string) string
	Int(name string) int
	IsSet(name string) bool
}

func billingPricingPlanPeriodsUpdateAction(ctx context.Context, cmd billingPricingPlanPeriodsUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("period ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	periodID := cmd.Args().First()

	req := admin.PricingPlanPeriodUpdateRequest{}

	if cmd.IsSet("price") {
		req.PriceUsd = float32(cmd.Float("price"))
	}
	if cmd.IsSet("cadence") {
		req.Cadence = cmd.String("cadence")
	}
	if cmd.IsSet("quota-plan-id") {
		req.QuotaPlanId = cmd.Int("quota-plan-id")
	}
	if cmd.IsSet("rolling-days") {
		rollingDays := cmd.Int("rolling-days")
		req.RollingDays = &rollingDays
	}

	period, err := service.UpdatePricingPlanPeriod(ctx, periodID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(period)
	}

	output.Printfln("Pricing plan period updated successfully:")
	output.Printfln("  ID: %d", period.Id)
	output.Printfln("  Plan ID: %d", period.PricingPlanId)
	output.Printfln("  Price: %.2f USD", period.PriceUsd)
	output.Printfln("  Cadence: %s", period.Cadence)
	return nil
}

func newBillingPricingPlanPeriodsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a pricing plan period",
		Description: `Delete a pricing plan period by ID.

Examples:
  pinner admin billing pricing-plan-periods delete <id>
  pinner admin billing pricing-plan-periods delete <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlanPeriodsDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlanPeriodsDeleteCmdGetter defines the interface for getting delete command args.
type billingPricingPlanPeriodsDeleteCmdGetter interface {
	Args() cli.Args
}

func billingPricingPlanPeriodsDeleteAction(ctx context.Context, cmd billingPricingPlanPeriodsDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("period ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	periodID := cmd.Args().First()
	if err := service.DeletePricingPlanPeriod(ctx, periodID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":    "deleted",
			"period_id": periodID,
		})
	}

	output.Printfln("Pricing plan period %s deleted successfully", periodID)
	return nil
}
