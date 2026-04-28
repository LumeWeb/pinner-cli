package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newBillingOverviewCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdOverview,
		Usage: "Show billing entity overview and relationships",
		Description: `Display an overview of billing entities and their relationships.

Shows the data model hierarchy and current entity counts.

Examples:
  pinner admin billing overview
  pinner admin billing overview --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingOverviewAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory, defaultQuotaAdminServiceFactory)
		},
	}
}

// billingOverviewCmdGetter defines the interface for the overview command.
type billingOverviewCmdGetter interface{}

func billingOverviewAction(ctx context.Context, cmd billingOverviewCmdGetter, output Output, cfgMgr config.Manager, billingFactory BillingAdminServiceFactory, quotaFactory QuotaAdminServiceFactory) error {
	billingService := billingFactory(cfgMgr, output)
	if err := billingService.RequireAuthenticated(); err != nil {
		return err
	}

	quotaService := quotaFactory(cfgMgr, output)

	quotaPlans, quotaTotal, err := quotaService.ListPlans(ctx)
	if err != nil {
		return fmt.Errorf("failed to list quota plans: %w", err)
	}

	priceLines, priceLinesTotal, err := billingService.ListPriceLines(ctx)
	if err != nil {
		return fmt.Errorf("failed to list price lines: %w", err)
	}

	pricingPlans, pricingPlansTotal, err := billingService.ListPricingPlans(ctx)
	if err != nil {
		return fmt.Errorf("failed to list pricing plans: %w", err)
	}

	periods, periodsTotal, err := billingService.ListPricingPlanPeriods(ctx)
	if err != nil {
		return fmt.Errorf("failed to list pricing plan periods: %w", err)
	}

	activeQuotaPlans := 0
	for _, p := range quotaPlans {
		if p.IsActive {
			activeQuotaPlans++
		}
	}

	activePriceLines := 0
	for _, pl := range priceLines {
		if pl.IsActive {
			activePriceLines++
		}
	}

	activePricingPlans := 0
	for _, pp := range pricingPlans {
		if pp.IsActive {
			activePricingPlans++
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"entity_relationships": map[string]string{
				"quota_plan":          "<-- pricing_plan_period --> pricing_plan",
				"pricing_plan":        "--> price_line",
				"price_line":          "--> (ordered list of pricing_plans)",
			},
			"counts": map[string]any{
				"quota_plans": map[string]any{
					"total":  quotaTotal,
					"active": activeQuotaPlans,
				},
				"price_lines": map[string]any{
					"total":  priceLinesTotal,
					"active": activePriceLines,
				},
				"pricing_plans": map[string]any{
					"total":  pricingPlansTotal,
					"active": activePricingPlans,
				},
				"pricing_plan_periods": map[string]any{
					"total": periodsTotal,
				},
			},
		})
	}

	output.Printf("Entity Relationships:")
	output.Printf("  Quota Plan <── Pricing Plan Period ──> Pricing Plan")
	output.Printf("                                               │")
	output.Printf("                                          Price Line")
	output.Printf("                                               │")
	output.Printf("                                         (ordered list)")
	output.Printf("")
	output.Printf("  Quota Plans:         %d total (%d active)", quotaTotal, activeQuotaPlans)
	output.Printf("  Price Lines:         %d total (%d active)", priceLinesTotal, activePriceLines)
	output.Printf("  Pricing Plans:       %d total (%d active)", pricingPlansTotal, activePricingPlans)
	output.Printf("  Plan Periods:        %d total", periodsTotal)

	_ = periods
	return nil
}
