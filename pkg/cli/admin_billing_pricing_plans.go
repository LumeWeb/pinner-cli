package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

func newBillingPricingPlansListCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdList,
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

func newBillingPricingPlansGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get pricing plan by ID",
		Description: `Get details of a specific pricing plan.

Arguments:
  <plan-id>  The unique identifier of the pricing plan

Examples:
  pinner admin billing pricing-plans get 1
  pinner admin billing pricing-plans get 5 --json`,
		ArgsUsage: "<plan-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPricingPlansGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPricingPlansGetCmdGetter defines the interface for getting get command args.
type billingPricingPlansGetCmdGetter interface {
	Args() cli.Args
}

func billingPricingPlansGetAction(ctx context.Context, cmd billingPricingPlansGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("plan ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	planID := cmd.Args().First()
	plan, err := service.GetPricingPlan(ctx, planID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(plan)
	}

	output.PrintFields(FieldGroup{
		Title: "Pricing Plan Details:",
		Fields: []Field{
			{Label: "ID", Value: strconv.FormatInt(int64(plan.Id), 10)},
			{Label: "Name", Value: plan.Name},
			{Label: "Currency", Value: plan.Currency},
			{Label: "Active", Value: strconv.FormatBool(plan.IsActive)},
			{Label: "Public", Value: strconv.FormatBool(plan.IsPublic)},
		},
	})

	if plan.Description != "" {
		output.Printf("\n  Description: %s\n", plan.Description)
	}

	if len(plan.PricingPeriods) > 0 {
		output.Printf("\n")
		output.PrintFields(FieldGroup{
			Title: fmt.Sprintf("Pricing Periods (%d):", len(plan.PricingPeriods)),
		})
		headers := []string{"ID", "Price", "Cadence", "Quota Plan ID", "Active"}
		rows := make([][]string, len(plan.PricingPeriods))
		for i, p := range plan.PricingPeriods {
			quotaPlanID := strconv.FormatInt(int64(p.QuotaPlanId), 10)
			rows[i] = []string{
				fmt.Sprintf("%d", p.Id),
				FormatUSD(p.PriceUsd),
				p.Cadence,
				quotaPlanID,
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

Optionally create a pricing plan period in the same command by providing:
  --quota-plan-id: Creates a period with the specified quota plan
  --price: Required with --quota-plan-id, price in USD
  --cadence: Required with --quota-plan-id, cadence (monthly, yearly, rolling)
  --rolling-days: Optional, for rolling cadence only
  --allow-free: Optional, allows $0 price

Examples:
  pinner admin billing pricing-plans create --name "Pro Plan" --currency USD
  pinner admin billing pricing-plans create --name "Basic" --currency USD --description "Basic plan" --is-active
  pinner admin billing pricing-plans create --name "Premium" --currency USD --is-public --json
  # Create plan with period in one command:
  pinner admin billing pricing-plans create --name "Starter" --currency USD --quota-plan-id 1 --price 9.99 --cadence monthly
  pinner admin billing pricing-plans create --name "Annual" --currency USD --quota-plan-id 2 --price 99.99 --cadence yearly`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagName,
				Usage:    "Pricing plan name",
				Required: true,
			},
			&cli.StringFlag{
				Name:     FlagCurrency,
				Usage:    "Currency code (e.g., USD, EUR)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  FlagDescription,
				Usage: "Plan description",
			},
			&cli.BoolFlag{
				Name:  FlagIsActive,
				Usage: "Mark plan as active",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  FlagIsPublic,
				Usage: "Mark plan as public",
				Value: false,
			},
			&cli.IntFlag{
				Name:  FlagPricelineID,
				Usage: "Associated price line ID",
			},
			// Pricing plan period creation flags (for creating both in one step)
			&cli.IntFlag{
				Name:  FlagQuotaPlanID,
				Usage: "Create a period: associated quota plan ID",
			},
			&cli.FloatFlag{
				Name:  FlagPrice,
				Usage: "Create a period: price in USD",
			},
			&cli.StringFlag{
				Name:  FlagCadence,
				Usage: "Create a period: cadence (e.g., monthly, yearly, rolling)",
			},
			&cli.IntFlag{
				Name:  FlagRollingDays,
				Usage: "Create a period: rolling days (for rolling cadence only)",
			},
			&cli.BoolFlag{
				Name:  FlagAllowFree,
				Usage: "Create a period: allow $0 price",
				Value: false,
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
	Float(name string) float64
	IsSet(name string) bool
}

func billingPricingPlansCreateAction(ctx context.Context, cmd billingPricingPlansCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := admin.PricingPlanCreateRequest{
		Name:           cmd.String(FlagName),
		Currency:       cmd.String(FlagCurrency),
		IsActive:       cmd.Bool(FlagIsActive),
		IsPublic:       cmd.Bool(FlagIsPublic),
		Description:    cmd.String(FlagDescription),
		PricingPeriods: []admin.PricingPlanPeriod{},
	}

	if cmd.IsSet(FlagPricelineID) {
		pricelineId := cmd.Int(FlagPricelineID)
		req.PricelineId = &pricelineId
	}

	plan, err := service.CreatePricingPlan(ctx, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	// Optionally create a pricing plan period if the required flags are set
	var period *admin.PricingPlanPeriod
	if cmd.IsSet(FlagQuotaPlanID) {
		if !cmd.IsSet(FlagPrice) || !cmd.IsSet(FlagCadence) {
			output.Printfln("\nWarning: --%s was set but --%s and/or --%s were not set. Period creation skipped.", FlagQuotaPlanID, FlagPrice, FlagCadence)
		} else {
			price := cmd.Float(FlagPrice)
			if price <= 0 && !cmd.Bool(FlagAllowFree) {
				output.Printfln("\nWarning: --%s must be greater than 0 (use --%s for $0 plans). Period creation skipped.", FlagPrice, FlagAllowFree)
			} else {
				periodReq := admin.PricingPlanPeriodCreateRequest{
					PricingPlanId: int(plan.Id),
					PriceUsd:      float32(price),
					Cadence:       cmd.String(FlagCadence),
					QuotaPlanId:   cmd.Int(FlagQuotaPlanID),
				}
				if cmd.IsSet(FlagRollingDays) {
					rollingDays := cmd.Int(FlagRollingDays)
					periodReq.RollingDays = &rollingDays
				}
				if cmd.Bool(FlagAllowFree) {
					periodReq.AllowFree = new(true)
				}
				period, err = service.CreatePricingPlanPeriod(ctx, &periodReq)
				if err != nil {
					output.Printfln("\nWarning: Failed to create pricing plan period: %v", err)
				}
			}
		}
	}

	if output.IsJSON() {
		if period != nil {
			return output.PrintJSON(map[string]any{
				"plan":   plan,
				"period": period,
			})
		}
		return output.PrintJSON(plan)
	}

	output.PrintFields(FieldGroup{
		Title: "Pricing plan created successfully:",
		Fields: []Field{
			{Label: "ID", Value: strconv.FormatInt(int64(plan.Id), 10)},
			{Label: "Name", Value: plan.Name},
			{Label: "Currency", Value: plan.Currency},
			{Label: "Active", Value: strconv.FormatBool(plan.IsActive)},
		},
	})

	if period != nil {
		output.Printf("\n")
		output.PrintFields(FieldGroup{
			Title: "Pricing plan period created successfully:",
			Fields: []Field{
				{Label: "ID", Value: strconv.FormatInt(int64(period.Id), 10)},
				{Label: "Plan ID", Value: strconv.FormatInt(int64(period.PricingPlanId), 10)},
				{Label: "Price", Value: FormatUSD(period.PriceUsd)},
				{Label: "Cadence", Value: string(period.Cadence)},
			},
		})
	}

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
				Name:  FlagName,
				Usage: "Pricing plan name",
			},
			&cli.StringFlag{
				Name:  FlagCurrency,
				Usage: "Currency code (e.g., USD, EUR)",
			},
			&cli.StringFlag{
				Name:  FlagDescription,
				Usage: "Plan description",
			},
			&cli.BoolFlag{
				Name:  FlagIsActive,
				Usage: "Mark plan as active",
			},
			&cli.BoolFlag{
				Name:  FlagIsPublic,
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

	if cmd.IsSet(FlagName) {
		req.Name = cmd.String(FlagName)
	}
	if cmd.IsSet(FlagCurrency) {
		req.Currency = cmd.String(FlagCurrency)
	}
	if cmd.IsSet(FlagDescription) {
		req.Description = cmd.String(FlagDescription)
	}
	if cmd.IsSet(FlagIsActive) {
		req.IsActive = cmd.Bool(FlagIsActive)
	}
	if cmd.IsSet(FlagIsPublic) {
		req.IsPublic = cmd.Bool(FlagIsPublic)
	}

	plan, err := service.UpdatePricingPlan(ctx, planID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(plan)
	}

	output.PrintFields(FieldGroup{
		Title: "Pricing plan updated successfully:",
		Fields: []Field{
			{Label: "ID", Value: strconv.FormatInt(int64(plan.Id), 10)},
			{Label: "Name", Value: plan.Name},
			{Label: "Currency", Value: plan.Currency},
			{Label: "Active", Value: strconv.FormatBool(plan.IsActive)},
		},
	})
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
				Name:     FlagPlanID,
				Usage:    "Pricing plan ID",
				Required: true,
			},
			&cli.FloatFlag{
				Name:     FlagPrice,
				Usage:    "Price in USD",
				Required: true,
			},
			&cli.StringFlag{
				Name:     FlagCadence,
				Usage:    "Cadence (e.g., monthly, yearly)",
				Required: true,
			},
			&cli.IntFlag{
				Name:     FlagQuotaPlanID,
				Usage:    "Associated quota plan ID",
				Required: true,
			},
			&cli.IntFlag{
				Name:  FlagRollingDays,
				Usage: "Rolling days (for rolling periods)",
			},
			&cli.BoolFlag{
				Name:  FlagAllowFree,
				Usage: "Allow $0 price (free plan)",
				Value: false,
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
	Bool(name string) bool
	IsSet(name string) bool
}

func billingPricingPlanPeriodsCreateAction(ctx context.Context, cmd billingPricingPlanPeriodsCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	price := cmd.Float(FlagPrice)
	if price <= 0 && !cmd.Bool(FlagAllowFree) {
		return fmt.Errorf("--price must be greater than 0 (use --allow-free for $0 plans)")
	}

	req := admin.PricingPlanPeriodCreateRequest{
		PricingPlanId: cmd.Int(FlagPlanID),
		PriceUsd:      float32(price),
		Cadence:       cmd.String(FlagCadence),
		QuotaPlanId:   cmd.Int(FlagQuotaPlanID),
	}

	if cmd.IsSet(FlagRollingDays) {
		rollingDays := cmd.Int(FlagRollingDays)
		req.RollingDays = &rollingDays
	}

	if cmd.Bool(FlagAllowFree) {
		req.AllowFree = new(true)
	}

	period, err := service.CreatePricingPlanPeriod(ctx, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(period)
	}

	output.PrintFields(FieldGroup{
		Title: "Pricing plan period created successfully:",
		Fields: []Field{
			{Label: "ID", Value: strconv.FormatInt(int64(period.Id), 10)},
			{Label: "Plan ID", Value: strconv.FormatInt(int64(period.PricingPlanId), 10)},
			{Label: "Price", Value: FormatUSD(period.PriceUsd)},
			{Label: "Cadence", Value: string(period.Cadence)},
		},
	})
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
				Name:  FlagPrice,
				Usage: "Price in USD",
			},
			&cli.StringFlag{
				Name:  FlagCadence,
				Usage: "Cadence (e.g., monthly, yearly)",
			},
			&cli.IntFlag{
				Name:  FlagQuotaPlanID,
				Usage: "Associated quota plan ID",
			},
			&cli.IntFlag{
				Name:  FlagRollingDays,
				Usage: "Rolling days (for rolling periods)",
			},
			&cli.BoolFlag{
				Name:  FlagAllowFree,
				Usage: "Allow $0 price (free plan)",
				Value: false,
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
	Bool(name string) bool
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

	if cmd.IsSet(FlagPrice) {
		req.PriceUsd = float32(cmd.Float(FlagPrice))
	}
	if cmd.IsSet(FlagCadence) {
		req.Cadence = cmd.String(FlagCadence)
	}
	if cmd.IsSet(FlagQuotaPlanID) {
		req.QuotaPlanId = cmd.Int(FlagQuotaPlanID)
	}
	if cmd.IsSet(FlagRollingDays) {
		rollingDays := cmd.Int(FlagRollingDays)
		req.RollingDays = &rollingDays
	}
	if cmd.Bool(FlagAllowFree) {
		req.AllowFree = new(true)
	}

	period, err := service.UpdatePricingPlanPeriod(ctx, periodID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(period)
	}

	output.PrintFields(FieldGroup{
		Title: "Pricing plan period updated successfully:",
		Fields: []Field{
			{Label: "ID", Value: strconv.FormatInt(int64(period.Id), 10)},
			{Label: "Plan ID", Value: strconv.FormatInt(int64(period.PricingPlanId), 10)},
			{Label: "Price", Value: FormatUSD(period.PriceUsd)},
			{Label: "Cadence", Value: string(period.Cadence)},
		},
	})
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

func newBillingSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSync,
		Usage: "Sync a pricing plan with payment gateway",
		Description: `Trigger immediate synchronization of a specific pricing plan with the payment gateway.

This command syncs a single pricing plan to ensure the payment gateway has the latest configuration.

Arguments:
  <plan-id>  The unique identifier of the pricing plan to sync

Examples:
  pinner admin billing pricing-plans sync <plan-id>
  pinner admin billing pricing-plans sync 123 --json`,
		ArgsUsage: "<plan-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSyncPricingPlanAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSyncPricingPlanCmdGetter defines the interface for getting sync command args.
type billingSyncPricingPlanCmdGetter interface {
	Args() cli.Args
}

func billingSyncPricingPlanAction(ctx context.Context, cmd billingSyncPricingPlanCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("plan ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	planID := cmd.Args().First()
	if err := service.SyncPricingPlan(ctx, planID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":  "success",
			"plan_id": planID,
			"message": fmt.Sprintf("Pricing plan %s synced successfully", planID),
		})
	}

	output.Printfln("Pricing plan %s synced successfully", planID)
	return nil
}

func newBillingSyncAllCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSyncAll,
		Usage: "Sync all pricing plans with payment gateways",
		Description: `Trigger synchronization of all pricing plans with payment gateways.

This command syncs all pricing plans to ensure the payment gateways have the latest configurations.

Examples:
  pinner admin billing pricing-plans sync-all
  pinner admin billing pricing-plans sync-all --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSyncAllPricingPlansAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSyncAllPricingPlansCmdGetter is an empty interface for sync-all command (no args needed).
type billingSyncAllPricingPlansCmdGetter interface{}

func billingSyncAllPricingPlansAction(ctx context.Context, cmd billingSyncAllPricingPlansCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	if err := service.SyncAllPricingPlans(ctx); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":  "success",
			"message": "All pricing plans synced successfully",
		})
	}

	output.Print("All pricing plans synced successfully")
	return nil
}
