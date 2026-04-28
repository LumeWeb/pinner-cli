package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

func newBillingSubscribersListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all subscribers",
		Description: `List all billing subscribers across all gateways.

Examples:
  pinner admin billing subscribers list
  pinner admin billing subscribers list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersListAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersListCmdGetter is an empty interface for list command (no args/flags needed)
type billingSubscribersListCmdGetter interface{}

func billingSubscribersListAction(ctx context.Context, cmd billingSubscribersListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	subscribers, total, err := service.ListSubscribers(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"subscribers": subscribers,
			"total":       total,
		})
	}

	output.Printfln("Total subscribers: %d", total)
	if len(subscribers) > 0 {
		headers := []string{"ID", "User ID", "Ext ID", "Gateway", "Active", "Payment", "Sub ID"}
		rows := make([][]string, len(subscribers))
		for i, sub := range subscribers {
			payment := "-"
			if sub.PaymentStatus != nil {
				payment = *sub.PaymentStatus
			}
			rows[i] = []string{
				fmt.Sprintf("%d", sub.Id),
				fmt.Sprintf("%d", sub.UserId),
				sub.ExternalId,
				sub.GatewayType,
				fmt.Sprintf("%t", sub.IsActive),
				payment,
				sub.SubscriptionId,
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingSubscribersGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get subscriber by ID",
		Description: `Get details of a specific subscriber.

Examples:
  pinner admin billing subscribers get <id>
  pinner admin billing subscribers get <id> --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersGetAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersGetCmdGetter defines the interface for getting get command args.
type billingSubscribersGetCmdGetter interface {
	Args() cli.Args
}

func billingSubscribersGetAction(ctx context.Context, cmd billingSubscribersGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("subscriber ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	subscriberID := cmd.Args().First()
	subscriber, err := service.GetSubscriber(ctx, subscriberID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(subscriber)
	}

	output.Printfln("Subscriber ID: %d", subscriber.Id)
	output.Printfln("User ID: %d", subscriber.UserId)
	output.Printfln("External ID: %s", subscriber.ExternalId)
	output.Printfln("Gateway Type: %s", subscriber.GatewayType)
	output.Printfln("Subscription ID: %s", subscriber.SubscriptionId)
	output.Printfln("Active: %t", subscriber.IsActive)

	if subscriber.PaymentStatus != nil {
		output.Printfln("Payment Status: %s", *subscriber.PaymentStatus)
	}
	if subscriber.PreviousPlanId != nil {
		output.Printfln("Previous Plan ID: %d", *subscriber.PreviousPlanId)
	}
	if subscriber.PricingPlanPeriodId != nil {
		output.Printfln("Plan Period ID: %d", *subscriber.PricingPlanPeriodId)
	}
	if subscriber.BillingPeriodStart != nil {
		output.Printfln("Billing Period Start: %s", subscriber.BillingPeriodStart.Format("2006-01-02 15:04:05"))
	}
	if subscriber.BillingPeriodEnd != nil {
		output.Printfln("Billing Period End: %s", subscriber.BillingPeriodEnd.Format("2006-01-02 15:04:05"))
	}
	if subscriber.CancelledAt != nil {
		output.Printfln("Cancelled At: %s", subscriber.CancelledAt.Format("2006-01-02 15:04:05"))
	}
	if subscriber.WillCancelAt != nil {
		output.Printfln("Will Cancel At: %s", subscriber.WillCancelAt.Format("2006-01-02 15:04:05"))
	}
	output.Printfln("Created At: %s", subscriber.CreatedAt.Format("2006-01-02 15:04:05"))
	output.Printfln("Updated At: %s", subscriber.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}

func newBillingSubscribersListGatewayCommand() *cli.Command {
	return &cli.Command{
		Name:  "list-gateway",
		Usage: "List subscribers by gateway",
		Description: `List all subscribers for a specific gateway.

Examples:
  pinner admin billing subscribers list-gateway <gateway-id>
  pinner admin billing subscribers list-gateway <gateway-id> --json`,
		ArgsUsage: "<gateway-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersListGatewayAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersListGatewayCmdGetter defines the interface for getting list-gateway command args.
type billingSubscribersListGatewayCmdGetter interface {
	Args() cli.Args
}

func billingSubscribersListGatewayAction(ctx context.Context, cmd billingSubscribersListGatewayCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("gateway ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	gatewayID := cmd.Args().First()
	subscribers, total, err := service.ListGatewaySubscribers(ctx, gatewayID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"subscribers": subscribers,
			"total":       total,
			"gateway_id":  gatewayID,
		})
	}

	output.Printfln("Gateway: %s", gatewayID)
	output.Printfln("Total subscribers: %d", total)
	if len(subscribers) > 0 {
		headers := []string{"ID", "User ID", "Ext ID", "Active", "Payment", "Sub ID"}
		rows := make([][]string, len(subscribers))
		for i, sub := range subscribers {
			payment := "-"
			if sub.PaymentStatus != nil {
				payment = *sub.PaymentStatus
			}
			rows[i] = []string{
				fmt.Sprintf("%d", sub.Id),
				fmt.Sprintf("%d", sub.UserId),
				sub.ExternalId,
				fmt.Sprintf("%t", sub.IsActive),
				payment,
				sub.SubscriptionId,
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingSubscribersListUserCommand() *cli.Command {
	return &cli.Command{
		Name:  "list-user",
		Usage: "List subscribers by user",
		Description: `List all subscriptions for a specific user.

Examples:
  pinner admin billing subscribers list-user <user-id>
  pinner admin billing subscribers list-user <user-id> --json`,
		ArgsUsage: "<user-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersListUserAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersListUserCmdGetter defines the interface for getting list-user command args.
type billingSubscribersListUserCmdGetter interface {
	Args() cli.Args
}

func billingSubscribersListUserAction(ctx context.Context, cmd billingSubscribersListUserCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("user ID is required")
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.Args().First()
	subscribers, total, err := service.GetUserSubscribers(ctx, userID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"subscribers": subscribers,
			"total":       total,
			"user_id":     userID,
		})
	}

	output.Printfln("User ID: %s", userID)
	output.Printfln("Total subscriptions: %d", total)
	if len(subscribers) > 0 {
		headers := []string{"ID", "Gateway", "Ext ID", "Active", "Payment", "Sub ID"}
		rows := make([][]string, len(subscribers))
		for i, sub := range subscribers {
			payment := "-"
			if sub.PaymentStatus != nil {
				payment = *sub.PaymentStatus
			}
			rows[i] = []string{
				fmt.Sprintf("%d", sub.Id),
				sub.GatewayType,
				sub.ExternalId,
				fmt.Sprintf("%t", sub.IsActive),
				payment,
				sub.SubscriptionId,
			}
		}
		output.PrintTable(headers, rows)
	}
	return nil
}

func newBillingSubscribersCancelCommand() *cli.Command {
	return &cli.Command{
		Name:  "cancel",
		Usage: "Cancel subscription",
		Description: `Cancel a user's subscription.

Examples:
  pinner admin billing subscribers cancel --user-id 123
  pinner admin billing subscribers cancel --user-id 123 --mode immediate
  pinner admin billing subscribers cancel --user-id 123 --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "mode",
				Usage: "Cancel mode: immediate, end_of_billing_period, etc.",
				Value: "end_of_billing_period",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersCancelAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersCancelCmdGetter defines the interface for getting cancel command flags.
type billingSubscribersCancelCmdGetter interface {
	String(name string) string
}

func billingSubscribersCancelAction(ctx context.Context, cmd billingSubscribersCancelCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.String("user-id")
	mode := cmd.String("mode")
	req := &admin.CancelSubscriptionRequest{
		Mode: &mode,
	}

	result, err := service.CancelUserSubscription(ctx, userID, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Subscription cancelled")
	output.Printfln("Action: %s", result.Action)
	if result.ConfirmationMessage != nil {
		output.Printfln("Confirmation: %s", *result.ConfirmationMessage)
	}
	if result.EffectiveTime != nil {
		output.Printfln("Effective Time: %s", result.EffectiveTime.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func newBillingSubscribersAbortCancelCommand() *cli.Command {
	return &cli.Command{
		Name:  "abort-cancel",
		Usage: "Abort scheduled subscription cancellation",
		Description: `Abort a scheduled subscription cancellation for a user.

Examples:
  pinner admin billing subscribers abort-cancel --user-id 123
  pinner admin billing subscribers abort-cancel --user-id 123 --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersAbortCancelAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersAbortCancelCmdGetter defines the interface for getting abort-cancel command flags.
type billingSubscribersAbortCancelCmdGetter interface {
	String(name string) string
}

func billingSubscribersAbortCancelAction(ctx context.Context, cmd billingSubscribersAbortCancelCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.String("user-id")
	result, err := service.AbortUserSubscriptionCancellation(ctx, userID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Cancellation aborted")
	output.Printfln("Action: %s", result.Action)
	if result.ConfirmationMessage != nil {
		output.Printfln("Confirmation: %s", *result.ConfirmationMessage)
	}
	if result.EffectiveTime != nil {
		output.Printfln("Effective Time: %s", result.EffectiveTime.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func newBillingSubscribersChangePlanCommand() *cli.Command {
	return &cli.Command{
		Name:  "change-plan",
		Usage: "Change subscription plan",
		Description: `Change a user's subscription plan.

Examples:
  pinner admin billing subscribers change-plan --user-id 123 --period-id 1
  pinner admin billing subscribers change-plan --user-id 123 --period-id 1 --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID",
				Required: true,
			},
			&cli.IntFlag{
				Name:     "period-id",
				Usage:    "New plan period ID",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersChangePlanAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersChangePlanCmdGetter defines the interface for getting change-plan command flags.
type billingSubscribersChangePlanCmdGetter interface {
	String(name string) string
	Int(name string) int
}

func billingSubscribersChangePlanAction(ctx context.Context, cmd billingSubscribersChangePlanCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.String("user-id")
	req := &admin.ChangePlanRequest{
		PeriodId: cmd.Int("period-id"),
	}

	result, err := service.ChangeUserPlan(ctx, userID, req)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Plan change initiated")
	output.Printfln("Action: %s", result.Action)
	if result.CheckoutLink != nil {
		output.Printfln("Checkout Link: %s", *result.CheckoutLink)
	}
	if result.EffectiveDate != nil {
		output.Printfln("Effective: %s", result.EffectiveDate.Format("2006-01-02 15:04:05"))
	}
	output.Printfln("Charge Due: %s", result.ChargeDue)
	output.Printfln("Credit Applied: %s", result.CreditApplied)
	return nil
}

func newBillingSubscribersPauseCommand() *cli.Command {
	return &cli.Command{
		Name:  "pause",
		Usage: "Pause subscription",
		Description: `Pause a user's subscription.

Examples:
  pinner admin billing subscribers pause --user-id 123
  pinner admin billing subscribers pause --user-id 123 --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersPauseAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersPauseCmdGetter defines the interface for getting pause command flags.
type billingSubscribersPauseCmdGetter interface {
	String(name string) string
}

func billingSubscribersPauseAction(ctx context.Context, cmd billingSubscribersPauseCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.String("user-id")
	result, err := service.PauseUserSubscription(ctx, userID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Subscription paused")
	output.Printfln("Action: %s", result.Action)
	if result.ConfirmationMessage != nil {
		output.Printfln("Confirmation: %s", *result.ConfirmationMessage)
	}
	if result.EffectiveTime != nil {
		output.Printfln("Effective Time: %s", result.EffectiveTime.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func newBillingSubscribersResumeCommand() *cli.Command {
	return &cli.Command{
		Name:  "resume",
		Usage: "Resume subscription",
		Description: `Resume a paused subscription for a user.

Examples:
  pinner admin billing subscribers resume --user-id 123
  pinner admin billing subscribers resume --user-id 123 --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "user-id",
				Usage:    "User ID",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return billingSubscribersResumeAction(ctx, newCLICommandWrapper(cmd), output, cfgMgr, defaultBillingAdminServiceFactory)
		},
	}
}

// billingSubscribersResumeCmdGetter defines the interface for getting resume command flags.
type billingSubscribersResumeCmdGetter interface {
	String(name string) string
}

func billingSubscribersResumeAction(ctx context.Context, cmd billingSubscribersResumeCmdGetter, output Output, cfgMgr config.Manager, serviceFactory BillingAdminServiceFactory) error {
	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	userID := cmd.String("user-id")
	result, err := service.ResumeUserSubscription(ctx, userID)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Subscription resumed")
	output.Printfln("Action: %s", result.Action)
	if result.ConfirmationMessage != nil {
		output.Printfln("Confirmation: %s", *result.ConfirmationMessage)
	}
	if result.EffectiveTime != nil {
		output.Printfln("Effective Time: %s", result.EffectiveTime.Format("2006-01-02 15:04:05"))
	}
	return nil
}
