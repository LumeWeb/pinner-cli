package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func newBillingOverviewCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdOverview,
		Usage: "Show billing entity overview and relationships",
		Description: `Display an overview of billing entities and their relationships.

This aggregates billing entities. For quota usage stats use 'admin quota stats' instead.

Shows the data model hierarchy and current entity counts.

Examples:
  pinner admin billing overview
  pinner admin billing overview --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingOverviewAction(ctx, output, cfgMgr, defaultBillingAdminServiceFactory, defaultQuotaAdminServiceFactory)
		},
	}
}

func billingOverviewAction(ctx context.Context, output Output, cfgMgr config.Manager, billingFactory BillingAdminServiceFactory, quotaFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
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
				"quota_plan":   "<-- pricing_plan_period --> pricing_plan",
				"pricing_plan": "--> price_line",
				"price_line":   "--> (ordered list of pricing_plans)",
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

	output.Printfln("Entity Relationships:")
	output.Printfln("  Quota Plan <── Pricing Plan Period ──> Pricing Plan")
	output.Printfln("                                               │")
	output.Printfln("                                          Price Line")
	output.Printfln("                                               │")
	output.Printfln("                                         (ordered list)")
	output.PrintFields(FieldGroup{
		PadTop: 1,
		Fields: []Field{
			{"Quota Plans", fmt.Sprintf("%d total (%d active)", quotaTotal, activeQuotaPlans)},
			{"Price Lines", fmt.Sprintf("%d total (%d active)", priceLinesTotal, activePriceLines)},
			{"Pricing Plans", fmt.Sprintf("%d total (%d active)", pricingPlansTotal, activePricingPlans)},
			{"Plan Periods", fmt.Sprintf("%d total", periodsTotal)},
		},
	})

	_ = periods
	return nil
}
