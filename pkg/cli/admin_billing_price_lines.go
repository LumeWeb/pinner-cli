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
		Name:  "price-lines",
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
		Name:  "list",
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

// billingPriceLinesListCmdGetter is an empty interface for list command (no args/flags needed)
type billingPriceLinesListCmdGetter interface{}

func billingPriceLinesListAction(ctx context.Context, cmd billingPriceLinesListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	priceLines, total, err := service.ListPriceLines(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"price_lines": priceLines,
			"total":       total,
		})
	}

	output.Printfln("Total price lines: %d", total)
	if len(priceLines) > 0 {
		headers := []string{"ID", "Name", "Description", "Active", "Default"}
		rows := make([][]string, len(priceLines))
		for i, pl := range priceLines {
			desc := ""
			if pl.Description != "" {
				desc = pl.Description
			}
			rows[i] = []string{
				fmt.Sprintf("%d", pl.Id),
				pl.Name,
				desc,
				fmt.Sprintf("%t", pl.IsActive),
				fmt.Sprintf("%t", pl.IsDefault),
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingPriceLinesGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get price line by ID",
		Description: `Get details of a specific price line.

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

// billingPriceLinesGetCmdGetter defines the interface for getting get command args.
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

	output.Printfln("Price Line ID: %d", priceLine.Id)
	output.Printfln("Name: %s", priceLine.Name)
	if priceLine.Description != "" {
		output.Printfln("Description: %s", priceLine.Description)
	}
	output.Printfln("Active: %t", priceLine.IsActive)
	output.Printfln("Default: %t", priceLine.IsDefault)
	output.Printfln("Created At: %s", priceLine.CreatedAt.Format("2006-01-02 15:04:05"))
	output.Printfln("Updated At: %s", priceLine.UpdatedAt.Format("2006-01-02 15:04:05"))

	if len(priceLine.Plans) > 0 {
		output.Printfln("\nAssociated Plans:")
		headers := []string{"ID", "Name", "Description", "Active", "Position"}
		rows := make([][]string, len(priceLine.Plans))
		for i, plan := range priceLine.Plans {
			position := ""
			if plan.Position != nil {
				position = fmt.Sprintf("%d", *plan.Position)
			}
			rows[i] = []string{
				fmt.Sprintf("%d", plan.Id),
				plan.Name,
				plan.Description,
				fmt.Sprintf("%t", plan.IsActive),
				position,
			}
		}
		output.PrintTable(headers, rows)
	}

	return nil
}

func newBillingPriceLinesCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a price line",
		Description: `Create a new billing price line.

Examples:
  pinner admin billing price-lines create --name "Storage" --description "Storage pricing"
  pinner admin billing price-lines create --name "Bandwidth" --description "Monthly bandwidth" --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "name",
				Usage:    "Price line name",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Price line description",
			},
			&cli.BoolFlag{
				Name:  "is-active",
				Usage: "Mark price line as active",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "is-default",
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

// billingPriceLinesCreateCmdGetter defines the interface for getting create command flags.
type billingPriceLinesCreateCmdGetter interface {
	String(name string) string
	Bool(name string) bool
}

func billingPriceLinesCreateAction(ctx context.Context, cmd billingPriceLinesCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := admin.PriceLineCreateRequest{
		Name:      cmd.String("name"),
		IsActive:  cmd.Bool("is-active"),
		IsDefault: cmd.Bool("is-default"),
	}

	if v := cmd.String("description"); v != "" {
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

	output.Printfln("Price line created successfully:")
	output.Printfln("  ID: %d", priceLine.Id)
	output.Printfln("  Name: %s", priceLine.Name)
	output.Printfln("  Active: %t", priceLine.IsActive)
	return nil
}

func newBillingPriceLinesUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a price line",
		Description: `Update an existing price line.

Examples:
  pinner admin billing price-lines update <id> --name "Updated Storage"
  pinner admin billing price-lines update <id> --description "New description" --is-active false --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "name",
				Usage: "Price line name",
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Price line description",
			},
			&cli.BoolFlag{
				Name:  "is-active",
				Usage: "Mark price line as active",
			},
			&cli.BoolFlag{
				Name:  "is-default",
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

	req := admin.PriceLineUpdateRequest{}

	if cmd.IsSet("name") {
		req.Name = cmd.String("name")
	}
	if cmd.IsSet("description") {
		req.Description = cmd.String("description")
	}
	if cmd.IsSet("is-active") {
		req.IsActive = cmd.Bool("is-active")
	}
	if cmd.IsSet("is-default") {
		req.IsDefault = cmd.Bool("is-default")
	}

	priceLine, err := service.UpdatePriceLine(ctx, priceLineID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLine)
	}

	output.Printfln("Price line updated successfully:")
	output.Printfln("  ID: %d", priceLine.Id)
	output.Printfln("  Name: %s", priceLine.Name)
	output.Printfln("  Active: %t", priceLine.IsActive)
	return nil
}

func newBillingPriceLinesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a price line",
		Description: `Delete a price line by ID.

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

// billingPriceLinesDeleteCmdGetter defines the interface for getting delete command args.
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
		return output.PrintJSON(map[string]string{
			"status":        "deleted",
			"price_line_id": priceLineID,
		})
	}

	output.Printfln("Price line %s deleted successfully", priceLineID)
	return nil
}

func newBillingPriceLinesAddPlanCommand() *cli.Command {
	return &cli.Command{
		Name:  "add-plan",
		Usage: "Add plan to price line",
		Description: `Add a pricing plan to a price line.

Examples:
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id> --position 1
  pinner admin billing price-lines add-plan <id> --plan-id <plan-id> --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "plan-id",
				Usage:    "Pricing plan ID to add",
				Required: true,
			},
			&cli.IntFlag{
				Name:  "position",
				Usage: "Position of the plan in the price line",
				Value: 0,
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
	Int(name string) int
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
	planID := cmd.Int("plan-id")
	position := cmd.Int("position")

	req := admin.AddPlanToPriceLineRequest{
		PlanId:   planID,
		Position: position,
	}

	priceLine, err := service.AddPlanToPriceLine(ctx, priceLineID, &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(priceLine)
	}

	output.Printfln("Plan %d added to price line %s at position %d", planID, priceLineID, position)
	return nil
}

func newBillingPriceLinesDeletePlanCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete-plan",
		Usage: "Remove plan from price line",
		Description: `Remove a pricing plan from a price line.

Examples:
  pinner admin billing price-lines delete-plan <id> --plan-id <plan-id>
  pinner admin billing price-lines delete-plan <id> --plan-id <plan-id> --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "plan-id",
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
	Int(name string) int
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
	planID := cmd.Int("plan-id")

	if err := service.DeletePlanFromPriceLine(ctx, priceLineID, strconv.Itoa(planID)); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":        "plan_removed",
			"price_line_id": priceLineID,
			"plan_id":       strconv.Itoa(planID),
		})
	}

	output.Printfln("Plan %d removed from price line %s successfully", planID, priceLineID)
	return nil
}

func newBillingPriceLinesUpdatePlanPositionCommand() *cli.Command {
	return &cli.Command{
		Name:  "update-plan-position",
		Usage: "Update price line position",
		Description: `Update the position of a pricing plan within a price line.

Examples:
  pinner admin billing price-lines update-plan-position <id> --plan-id <plan-id> --position 2
  pinner admin billing price-lines update-plan-position <id> --plan-id <plan-id> --position 1 --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "plan-id",
				Usage:    "Pricing plan ID",
				Required: true,
			},
			&cli.IntFlag{
				Name:     "position",
				Usage:    "New position value",
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
	planID := cmd.Int("plan-id")
	position := cmd.Int("position")

	req := admin.UpdatePlanPositionRequest{
		Position: position,
	}

	_, err := service.UpdatePlanPosition(ctx, priceLineID, strconv.Itoa(planID), &req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"status":        "position_updated",
			"price_line_id": priceLineID,
			"plan_id":       planID,
			"position":      position,
		})
	}

	output.Printfln("Position updated for plan %d in price line %s to %d", planID, priceLineID, position)
	return nil
}
