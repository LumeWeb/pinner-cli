package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

func newBillingPriceLinesCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPriceLines,
		Usage: "Manage billing price lines",
		Description: `List, create, update, and delete billing price lines.

Examples:
  pinner admin billing price-lines list
  pinner admin billing price-lines get <id>
  pinner admin billing price-lines create --name "Storage" --description "Storage pricing"
  pinner admin billing price-lines update <id> --name "Updated Storage"
  pinner admin billing price-lines delete <id>
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id> --position 1
  pinner admin billing price-lines delete-plan <id> --plan-id <plan-id>
  pinner admin billing price-lines update-plan-position <id> --plan-id <plan-id> --position 2`,
		Commands: []*cli.Command{
			newBillingPriceLinesListCommand(),
			newBillingPriceLinesGetCommand(),
			newBillingPriceLinesCreateCommand(),
			newBillingPriceLinesUpdateCommand(),
			newBillingPriceLinesDeleteCommand(),
			newBillingPriceLinesAddPlanCommand(),
			newBillingPriceLinesDeletePlanCommand(),
			newBillingPriceLinesUpdatePlanPositionCommand(),
		},
	}
}

func newBillingPriceLinesListCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdList,
		Usage: "List all price lines",
		Description: `List all billing price lines.

Examples:
  pinner admin billing price-lines list
  pinner admin billing price-lines list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesListCmdGetter defines the interface for getting list command args and flags.
type billingPriceLinesListCmdGetter interface {
	Args() cli.Args
}

func billingPriceLinesListAction(ctx context.Context, cmd billingPriceLinesListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLines, _, err := service.ListPriceLines(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLines)
	}

	if len(priceLines) == 0 {
		output.Print("No price lines found")
		return nil
	}

	output.Print("Price Lines:")
	for _, pl := range priceLines {
		output.Printf("  ID: %d", pl.Id)
		output.Printf("    Name: %s", pl.Name)
		if pl.Description != "" {
			output.Printf("    Description: %s", pl.Description)
		}
		output.Printf("    Active: %t", pl.IsActive)
		output.Printf("    Default: %t", pl.IsDefault)
	}

	return nil
}

func newBillingPriceLinesGetCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdGet,
		Usage: "Get price line details",
		Description: `Get detailed information about a billing price line.

Examples:
  pinner admin billing price-lines get <id>
  pinner admin billing price-lines get <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesGetCmdGetter defines the interface for getting get command args and flags.
type billingPriceLinesGetCmdGetter interface {
	Args() cli.Args
}

func billingPriceLinesGetAction(ctx context.Context, cmd billingPriceLinesGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()
	priceLine, err := service.GetPriceLine(ctx, priceLineID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLine)
	}

	output.Print("Price Line Details:")
	output.Printf("  ID: %d", priceLine.Id)
	output.Printf("  Name: %s", priceLine.Name)
	if priceLine.Description != "" {
		output.Printf("  Description: %s", priceLine.Description)
	}
	output.Printf("  Active: %t", priceLine.IsActive)
	output.Printf("  Default: %t", priceLine.IsDefault)

	output.Print("\n  Plans:")
	for _, plan := range priceLine.Plans {
		output.Printf("    - Plan ID: %d", plan.Id)
	}

	return nil
}

func newBillingPriceLinesCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdCreate,
		Usage: "Create a price line",
		Description: `Create a new billing price line.

Examples:
  pinner admin billing price-lines create --name "Storage" --description "Storage pricing"
  pinner admin billing price-lines create --name "Bandwidth" --description "Monthly bandwidth" --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagName,
				Usage:    "Price line name",
				Required: true,
			},
			&cli.StringFlag{
				Name:  FlagDescription,
				Usage: "Price line description",
			},
			&cli.BoolFlag{
				Name:  FlagIsActive,
				Usage: "Mark price line as active",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  FlagIsDefault,
				Usage: "Mark as default price line",
				Value: false,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesCreateCmdGetter defines the interface for getting create command args and flags.
type billingPriceLinesCreateCmdGetter interface {
	Args() cli.Args
	String(name string) string
	Bool(name string) bool
}

func billingPriceLinesCreateAction(ctx context.Context, cmd billingPriceLinesCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := admin.PriceLineCreateRequest{
		Name:      cmd.String(FlagName),
		IsActive:  cmd.Bool(FlagIsActive),
		IsDefault: cmd.Bool(FlagIsDefault),
	}

	if v := cmd.String(FlagDescription); v != "" {
		req.Description = v
	}

	priceLine, err := service.CreatePriceLine(ctx, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLine)
	}

	output.Print("Price line created successfully:")
	output.Printf("  ID: %d", priceLine.Id)
	output.Printf("  Name: %s", priceLine.Name)
	if priceLine.Description != "" {
		output.Printf("  Description: %s", priceLine.Description)
	}
	output.Printf("  Active: %t", priceLine.IsActive)
	output.Printf("  Default: %t", priceLine.IsDefault)

	return nil
}

func newBillingPriceLinesUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdUpdate,
		Usage: "Update a price line",
		Description: `Update an existing price line.

Examples:
  pinner admin billing price-lines update <id> --name "Updated Storage"
  pinner admin billing price-lines update <id> --description "New description" --is-active false --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagName,
				Usage: "Price line name",
			},
			&cli.StringFlag{
				Name:  FlagDescription,
				Usage: "Price line description",
			},
			&cli.BoolFlag{
				Name:  FlagIsActive,
				Usage: "Mark price line as active",
			},
			&cli.BoolFlag{
				Name:  FlagIsDefault,
				Usage: "Mark as default price line",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesUpdateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesUpdateCmdGetter defines the interface for getting update command args and flags.
type billingPriceLinesUpdateCmdGetter interface {
	Args() cli.Args
	String(name string) string
	Bool(name string) bool
	IsSet(name string) bool
}

func billingPriceLinesUpdateAction(ctx context.Context, cmd billingPriceLinesUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()

	if err := requireUpdateFields(cmd, FlagName, FlagDescription, FlagIsActive, FlagIsDefault); err != nil {
		return err
	}

	req := admin.PriceLineUpdateRequest{}
	if cmd.IsSet(FlagName) {
		req.Name = cmd.String(FlagName)
	}
	if cmd.IsSet(FlagDescription) {
		req.Description = cmd.String(FlagDescription)
	}
	if cmd.IsSet(FlagIsActive) {
		req.IsActive = cmd.Bool(FlagIsActive)
	}
	if cmd.IsSet(FlagIsDefault) {
		req.IsDefault = cmd.Bool(FlagIsDefault)
	}

	priceLine, err := service.UpdatePriceLine(ctx, priceLineID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLine)
	}

	output.Print("Price line updated successfully:")
	output.Printf("  ID: %d", priceLine.Id)
	output.Printf("  Name: %s", priceLine.Name)
	if priceLine.Description != "" {
		output.Printf("  Description: %s", priceLine.Description)
	}
	output.Printf("  Active: %t", priceLine.IsActive)
	output.Printf("  Default: %t", priceLine.IsDefault)

	return nil
}

func newBillingPriceLinesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdDelete,
		Usage: "Delete a price line",
		Description: `Delete a billing price line.

Examples:
  pinner admin billing price-lines delete <id>
  pinner admin billing price-lines delete <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesDeleteCmdGetter defines the interface for getting delete command args and flags.
type billingPriceLinesDeleteCmdGetter interface {
	Args() cli.Args
}

func billingPriceLinesDeleteAction(ctx context.Context, cmd billingPriceLinesDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()
	if err := service.DeletePriceLine(ctx, priceLineID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{"status": "deleted"})
	}

	output.Print("Price line deleted successfully")
	return nil
}

func newBillingPriceLinesAddPlanCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdAddPlan,
		Usage: "Add plan to price line",
		Description: `Add a pricing plan to a price line.

If --position is omitted, the plan is appended to the end of the price line.

Examples:
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id>
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id> --position 1
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id> --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagPlanID,
				Usage:    "Pricing plan ID to add",
				Required: true,
			},
			&cli.IntFlag{
				Name:  FlagPosition,
				Usage: "Position of the plan in the price line (auto-appended if omitted)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesAddPlanAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesAddPlanCmdGetter defines the interface for getting add-plan command args and flags.
type billingPriceLinesAddPlanCmdGetter interface {
	Args() cli.Args
	String(name string) string
	Int(name string) int
	IsSet(name string) bool
}

func billingPriceLinesAddPlanAction(ctx context.Context, cmd billingPriceLinesAddPlanCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()
	planIDStr := cmd.String(FlagPlanID)
	planID, err := strconv.Atoi(planIDStr)
	if err != nil {
		return fmt.Errorf("invalid plan ID: %w", err)
	}

	req := &admin.AddPlanToPriceLineRequest{
		PlanId: planID,
	}

	if cmd.IsSet(FlagPosition) {
		req.Position = cmd.Int(FlagPosition)
	} else {
		priceLine, err := service.GetPriceLine(ctx, priceLineID)
		if err != nil {
			return fmt.Errorf("failed to determine auto-position: %w", err)
		}
		pos := 1
		if priceLine.Plans != nil {
			pos = len(priceLine.Plans) + 1
		}
		req.Position = pos
	}

	_, err = service.AddPlanToPriceLine(ctx, priceLineID, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]interface{}{
			"price_line_id": priceLineID,
			"plan_id":       planID,
			"position":      req.Position,
			"status":        "added",
		})
	}

	output.Print("Plan added to price line successfully")
	output.Printf("  Price Line: %s", priceLineID)
	output.Printf("  Plan ID: %d", planID)
	output.Printf("  Position: %d", req.Position)

	return nil
}

func newBillingPriceLinesDeletePlanCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdDeletePlan,
		Usage: "Remove plan from price line",
		Description: `Remove a pricing plan from a price line.

Examples:
  pinner admin billing price-lines delete-plan <id> --plan-id <plan-id>
  pinner admin billing price-lines delete-plan <id> --plan-id <plan-id> --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagPlanID,
				Usage:    "Pricing plan ID to remove",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesDeletePlanAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesDeletePlanCmdGetter defines the interface for getting delete-plan command args and flags.
type billingPriceLinesDeletePlanCmdGetter interface {
	Args() cli.Args
	String(name string) string
}

func billingPriceLinesDeletePlanAction(ctx context.Context, cmd billingPriceLinesDeletePlanCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()
	planID := cmd.String(FlagPlanID)
	if err := service.DeletePlanFromPriceLine(ctx, priceLineID, planID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{"status": "plan removed"})
	}

	output.Print("Plan removed from price line successfully")
	return nil
}

func newBillingPriceLinesUpdatePlanPositionCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdUpdatePlanPosition,
		Usage: "Update plan position in price line",
		Description: `Update the position of a pricing plan within a price line.

Examples:
  pinner admin billing price-lines update-plan-position <id> --plan-id <plan-id> --position 1
  pinner admin billing price-lines update-plan-position <id> --plan-id <plan-id> --position 2 --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagPlanID,
				Usage:    "Pricing plan ID to reposition",
				Required: true,
			},
			&cli.IntFlag{
				Name:     FlagPosition,
				Usage:    "New position for the plan",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingPriceLinesUpdatePlanPositionAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingPriceLinesUpdatePlanPositionCmdGetter defines the interface for getting update-plan-position command args and flags.
type billingPriceLinesUpdatePlanPositionCmdGetter interface {
	Args() cli.Args
	String(name string) string
	Int(name string) int
}

func billingPriceLinesUpdatePlanPositionAction(ctx context.Context, cmd billingPriceLinesUpdatePlanPositionCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("price line ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLineID := cmd.Args().First()
	planID := cmd.String(FlagPlanID)
	position := cmd.Int(FlagPosition)

	req := &admin.UpdatePlanPositionRequest{
		Position: position,
	}

	_, err := service.UpdatePlanPosition(ctx, priceLineID, planID, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]interface{}{
			"price_line_id": priceLineID,
			"plan_id":       planID,
			"new_position":  position,
			"status":        "position updated",
		})
	}

	output.Print("Plan position updated successfully")
	return nil
}
