package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

func newBillingCreditsCommand() *cli.Command {
	return &cli.Command{
		Name:  "credits",
		Usage: "Manage billing credits",
		Description: `Manage billing credits for users.

Examples:
  pinner admin billing credits list
  pinner admin billing credits get <id>
  pinner admin billing credits create --user-id 123 --amount 100.00 --type manual --direction credit
  pinner admin billing credits delete <id>
  pinner admin billing credits restore <id>
  pinner admin billing credits purge
  pinner admin billing credits user-balance <user-id>
  pinner admin billing credits user-deleted-credits <user-id>`,
		Commands: []*cli.Command{
			newBillingCreditsListCommand(),
			newBillingCreditsGetCommand(),
			newBillingCreditsCreateCommand(),
			newBillingCreditsDeleteCommand(),
			newBillingCreditsRestoreCommand(),
			newBillingCreditsPurgeCommand(),
			newBillingCreditsUserBalanceCommand(),
			newBillingCreditsUserDeletedCreditsCommand(),
		},
	}
}

func newBillingCreditsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List credits",
		Description: `List all billing credits with optional filtering.

Examples:
  pinner admin billing credits list
  pinner admin billing credits list --user-id 123
  pinner admin billing credits list --direction credit
  pinner admin billing credits list --type manual`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "user-id",
				Usage: "Filter by user ID",
			},
			&cli.StringFlag{
				Name:  "direction",
				Usage: "Filter by direction (credit, debit)",
			},
			&cli.StringFlag{
				Name:  "type",
				Usage: "Filter by type",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsListCmdGetter defines the interface for getting list command flags.
type billingCreditsListCmdGetter interface {
	String(name string) string
}

func billingCreditsListAction(ctx context.Context, cmd billingCreditsListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	params := &admin.GetApiBillingCreditsParams{}
	if v := cmd.String("user-id"); v != "" {
		params.FiltersUserIdEq = &v
	}
	if v := cmd.String("direction"); v != "" {
		params.DirectionEq = &v
	}
	if v := cmd.String("type"); v != "" {
		params.TypeEq = &v
	}

	credits, total, err := service.ListCredits(ctx, params)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"credits": credits,
			"total":   total,
		})
	}

	output.Printfln("Total credits: %d", total)
	if len(credits) > 0 {
		headers := []string{"ID", "User ID", "Amount", "Type", "Direction", "Description"}
		rows := make([][]string, len(credits))
		for i, c := range credits {
			desc := ""
			if c.Description != nil {
				desc = *c.Description
			}
			rows[i] = []string{
				c.Id.String(),
				fmt.Sprintf("%d", c.UserId),
				c.Amount.String(),
				c.Type,
				c.Direction,
				desc,
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingCreditsGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get credit by ID",
		Description: `Get details of a specific credit by its ID.

Examples:
  pinner admin billing credits get <id>
  pinner admin billing credits get <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsGetCmdGetter defines the interface for getting get command args.
type billingCreditsGetCmdGetter interface {
	Args() cli.Args
}

func billingCreditsGetAction(ctx context.Context, cmd billingCreditsGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("credit ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	creditID := cmd.Args().First()
	credit, err := service.GetCredit(ctx, creditID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(credit)
	}

	fields := []Field{
		{Label: "Credit ID", Value: credit.Id.String()},
		{Label: "User ID", Value: strconv.FormatInt(int64(credit.UserId), 10)},
		{Label: "Amount", Value: credit.Amount.String()},
		{Label: "Type", Value: string(credit.Type)},
		{Label: "Direction", Value: string(credit.Direction)},
		{Label: "Created At", Value: credit.CreatedAt.Format(time.RFC3339)},
	}
	if credit.Description != nil && *credit.Description != "" {
		fields = append(fields, Field{Label: "Description", Value: *credit.Description})
	}
	if credit.ReferenceId != nil && *credit.ReferenceId != "" {
		fields = append(fields, Field{Label: "Reference ID", Value: *credit.ReferenceId})
	}
	if credit.ReferenceType != nil && *credit.ReferenceType != "" {
		fields = append(fields, Field{Label: "Reference Type", Value: *credit.ReferenceType})
	}
	output.PrintFields(FieldGroup{Fields: fields})
	return nil
}

func newBillingCreditsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new credit",
		Description: `Create a new billing credit for a user.

Examples:
  pinner admin billing credits create --user-id 123 --amount 100.00 --type manual --direction credit
  pinner admin billing credits create --user-id 123 --amount 50.00 --type promo --direction credit --description "Promotional credit"
  pinner admin billing credits create --user-id 123 --amount 200.00 --type manual --direction debit --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID to credit",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "amount",
				Usage:    "Credit amount (as decimal string)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "type",
				Usage:    "Credit type (e.g., manual, promo, referral)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "direction",
				Usage:    "Direction (credit or debit)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Credit description",
			},
			&cli.StringFlag{
				Name:  "reference-id",
				Usage: "Reference ID for this credit",
			},
			&cli.StringFlag{
				Name:  "reference-type",
				Usage: "Reference type for this credit",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsCreateAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsCreateCmdGetter defines the interface for getting create command flags.
type billingCreditsCreateCmdGetter interface {
	String(name string) string
}

func billingCreditsCreateAction(ctx context.Context, cmd billingCreditsCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID, err := strconv.Atoi(cmd.String("user-id"))
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	req := &admin.CreditCreateRequest{
		UserId:    userID,
		Amount:    cmd.String("amount"),
		Type:      cmd.String("type"),
		Direction: cmd.String("direction"),
	}

	if v := cmd.String("description"); v != "" {
		req.Description = &v
	}
	if v := cmd.String("reference-id"); v != "" {
		req.ReferenceId = &v
	}
	if v := cmd.String("reference-type"); v != "" {
		req.ReferenceType = &v
	}

	credit, err := service.CreateCredit(ctx, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(credit)
	}

	output.PrintFields(FieldGroup{
		Title: "Credit created successfully:",
		Fields: []Field{
			{Label: "ID", Value: credit.Id.String()},
			{Label: "Amount", Value: credit.Amount.String()},
			{Label: "Type", Value: string(credit.Type)},
			{Label: "Direction", Value: string(credit.Direction)},
		},
	})
	return nil
}

func newBillingCreditsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete (soft-delete) a credit",
		Description: `Soft-delete a credit by its ID.

Examples:
  pinner admin billing credits delete <id>
  pinner admin billing credits delete <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsDeleteAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsDeleteCmdGetter defines the interface for getting delete command args.
type billingCreditsDeleteCmdGetter interface {
	Args() cli.Args
}

func billingCreditsDeleteAction(ctx context.Context, cmd billingCreditsDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("credit ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	creditID := cmd.Args().First()
	if err := service.DeleteCredit(ctx, creditID); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]string{
			"status":    "deleted",
			"credit_id": creditID,
		})
	}

	output.Printfln("Credit %s deleted successfully", creditID)
	return nil
}

func newBillingCreditsRestoreCommand() *cli.Command {
	return &cli.Command{
		Name:  "restore",
		Usage: "Restore a deleted credit",
		Description: `Restore a soft-deleted credit by its ID.

Examples:
  pinner admin billing credits restore <id>
  pinner admin billing credits restore <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsRestoreAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsRestoreCmdGetter defines the interface for getting restore command args.
type billingCreditsRestoreCmdGetter interface {
	Args() cli.Args
}

func billingCreditsRestoreAction(ctx context.Context, cmd billingCreditsRestoreCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("credit ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	creditID := cmd.Args().First()
	credit, err := service.RestoreCredit(ctx, creditID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(credit)
	}

	output.Printfln("Credit %s restored successfully", credit.Id.String())
	return nil
}

func newBillingCreditsPurgeCommand() *cli.Command {
	return &cli.Command{
		Name:  "purge",
		Usage: "Purge deleted credits",
		Description: `Permanently delete soft-deleted credits older than specified duration.

Examples:
  pinner admin billing credits purge
  pinner admin billing credits purge --older-than "30d"
  pinner admin billing credits purge --older-than "7d" --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "older-than",
				Usage: "Delete credits deleted more than this duration ago (e.g., 30d, 1w, 24h)",
				Value: "30d",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsPurgeAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsPurgeCmdGetter defines the interface for getting purge command flags.
type billingCreditsPurgeCmdGetter interface {
	String(name string) string
}

func billingCreditsPurgeAction(ctx context.Context, cmd billingCreditsPurgeCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	req := &admin.CreditPurgeRequest{
		OlderThan: cmd.String("older-than"),
	}

	count, err := service.PurgeCredits(ctx, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"purged": count,
		})
	}

	output.Printfln("Purged %d deleted credits", count)
	return nil
}

func newBillingCreditsUserBalanceCommand() *cli.Command {
	return &cli.Command{
		Name:  "user-balance",
		Usage: "Get user balance",
		Description: `Get the current balance for a specific user.

Examples:
  pinner admin billing credits user-balance <user-id>
  pinner admin billing credits user-balance <user-id> --json`,
		ArgsUsage: "<user-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsUserBalanceAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsUserBalanceCmdGetter defines the interface for getting user-balance command args.
type billingCreditsUserBalanceCmdGetter interface {
	Args() cli.Args
}

func billingCreditsUserBalanceAction(ctx context.Context, cmd billingCreditsUserBalanceCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("user ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.Args().First()
	balance, err := service.GetUserBalance(ctx, userID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(balance)
	}

	output.PrintFields(FieldGroup{
		Title: fmt.Sprintf("User Balance (%s):", userID),
		Fields: []Field{
			{Label: "Balance", Value: balance.Balance.String()},
		},
	})
	return nil
}

func newBillingCreditsUserDeletedCreditsCommand() *cli.Command {
	return &cli.Command{
		Name:  "user-deleted-credits",
		Usage: "Get deleted credits by user",
		Description: `Get all soft-deleted credits for a specific user.

Examples:
  pinner admin billing credits user-deleted-credits <user-id>
  pinner admin billing credits user-deleted-credits <user-id> --json`,
		ArgsUsage: "<user-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "direction",
				Usage: "Filter by direction",
			},
			&cli.StringFlag{
				Name:  "type",
				Usage: "Filter by type",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingCreditsUserDeletedCreditsAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingCreditsUserDeletedCreditsCmdGetter defines the interface for getting user-deleted-credits command args and flags.
type billingCreditsUserDeletedCreditsCmdGetter interface {
	Args() cli.Args
	String(name string) string
}

func billingCreditsUserDeletedCreditsAction(ctx context.Context, cmd billingCreditsUserDeletedCreditsCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("user ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.Args().First()
	params := &admin.GetApiBillingUsersUserIdDeletedCreditsParams{}
	if v := cmd.String("direction"); v != "" {
		params.DirectionEq = &v
	}
	if v := cmd.String("type"); v != "" {
		params.TypeEq = &v
	}

	credits, total, err := service.GetUserDeletedCredits(ctx, userID, params)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"credits": credits,
			"total":   total,
		})
	}

	output.Printfln("Deleted credits for user %s (total: %d):", userID, total)
	if len(credits) > 0 {
		headers := []string{"ID", "Amount", "Type", "Direction", "Description", "Deleted At"}
		rows := make([][]string, len(credits))
		for i, c := range credits {
			desc := ""
			if c.Description != nil {
				desc = *c.Description
			}
			deletedAt := ""
			if c.DeletedAt != nil {
				deletedAt = c.DeletedAt.Format(time.RFC3339)
			}
			rows[i] = []string{
				c.Id.String(),
				c.Amount.String(),
				c.Type,
				c.Direction,
				desc,
				deletedAt,
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}
