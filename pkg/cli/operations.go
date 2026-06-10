package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/pterm/pterm"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"github.com/urfave/cli/v3"
)

func newOperationsCommand() *cli.Command {
	return &cli.Command{
		Name:     "operations",
		Category: "Management",
		Usage:    "List and inspect account operations",
		Description: `View and monitor account operations such as uploads, pins, and other processing tasks.

Operations track server-side processing of your requests. Each operation has a status:
- pending   - Operation is queued
- running   - Operation is in progress
- completed - Operation finished successfully
- failed    - Operation failed
- error     - Operation encountered an error

Examples:
  pinner operations list
  pinner operations list --status running
  pinner operations list --cid QmHash --limit 20
  pinner operations get 42
  pinner operations get 42 --watch`,
		Commands: []*cli.Command{
			newOperationsListCommand(),
			newOperationsGetCommand(),
		},
	}
}

func newOperationsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List account operations",
		Description: `List your account operations with optional filtering.

Examples:
  pinner operations list
  pinner operations list --status running
  pinner operations list --operation upload
  pinner operations list --protocol ipfs
  pinner operations list --cid QmHash
  pinner operations list --limit 20
  pinner operations list --watch`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagStatus,
				Usage: "Filter by status (pending, running, completed, failed, error)",
			},
			&cli.StringFlag{
				Name:  FlagOperation,
				Usage: "Filter by operation type (e.g., upload, pin)",
			},
			&cli.StringFlag{
				Name:  FlagProtocol,
				Usage: "Filter by protocol (e.g., ipfs)",
			},
			&cli.StringFlag{
				Name:  FlagCID,
				Usage: "Filter by CID",
			},
			LimitFlag(),
			WatchFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return operationsList(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultAuthServiceFactory, defaultOperationsServiceFactory)
		},
	}
}

func newOperationsGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Get details of an operation",
		ArgsUsage: "<operation-id>",
		Description: `Get detailed information about a specific operation.

Examples:
  pinner operations get 42
  pinner operations get 42 --watch`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  FlagWatch,
				Usage: "Wait for the operation to complete",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return operationsGet(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultAuthServiceFactory, defaultOperationsServiceFactory)
		},
	}
}

type operationsCommandGetter interface {
	String(name string) string
	Int(name string) int
	Bool(name string) bool
	Args() cli.Args
}

func defaultOperationsServiceFactory(cfgMgr config.Manager, output Output, authService AuthService) OperationsService {
	return NewOperationsService(cfgMgr, output, authService)
}

func operationsList(ctx context.Context, cmd operationsCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory, serviceFactory OperationsServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	authService := authServiceFactory(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
	service := serviceFactory(cfgMgr, output, authService)

	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	opts := OperationsListOptions{
		StatusFilter:     cmd.String(FlagStatus),
		OperationFilter:  cmd.String(FlagOperation),
		ProtocolFilter:   cmd.String(FlagProtocol),
		CIDFilter:        cmd.String(FlagCID),
		Limit:            cmd.Int(FlagLimit),
	}

	watch := cmd.Bool(FlagWatch)

	if watch {
		return watchOperationsList(ctx, service, output, opts)
	}

	result, err := service.List(ctx, opts)
	if err != nil {
		return err
	}

	if len(result.Operations) == 0 {
		output.Printfln("No operations found")
		return nil
	}

	output.Printfln("Found %d operation(s)", result.Total)

	headers := []string{"ID", "OPERATION", "PROTOCOL", "STATUS", "CID", "PROGRESS", "STARTED"}
	rows := make([][]string, len(result.Operations))
	for i, op := range result.Operations {
		rows[i] = []string{
			fmt.Sprintf("%d", op.ID),
			op.OperationDisplayName,
			op.ProtocolDisplayName,
			formatOperationStatusWithColor(op.Status),
			op.CID,
			fmt.Sprintf("%.0f%%", op.ProgressPercent),
			op.StartedAt,
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

func operationsGet(ctx context.Context, cmd operationsCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory, serviceFactory OperationsServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	authService := authServiceFactory(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
	service := serviceFactory(cfgMgr, output, authService)

	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args().Slice()
	if len(args) == 0 {
		return fmt.Errorf("%w. Usage: pinner operations get <operation-id>", ErrOperationNotFound)
	}

	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
		return fmt.Errorf("invalid operation ID: %s", args[0])
	}

	watch := cmd.Bool(FlagWatch)

	if watch {
		return watchOperation(ctx, service, output, id)
	}

	op, err := service.Get(ctx, id)
	if err != nil {
		return err
	}

	return renderOperationDetail(output, op)
}

func watchOperationsList(ctx context.Context, service OperationsService, output Output, opts OperationsListOptions) error {
	output.Printfln("Watching (Press Ctrl+C to stop)...")

	headers := []string{"ID", "OPERATION", "PROTOCOL", "STATUS", "CID", "PROGRESS", "STARTED"}

	renderResults := func(result *OperationsListResult) {
		if output.IsJSON() {
			_ = output.PrintJSON(map[string]any{
				"type":    "watch",
				"title":   fmt.Sprintf("Found %d operation(s)", result.Total),
				"headers": headers,
				"rows":    buildOperationRows(result),
				"time":    time.Now().Format(time.RFC3339),
			})
		} else {
			pterm.Printo("\r")
			output.Printfln("Found %d operation(s) - Last updated: %s", result.Total, time.Now().Format("15:04:05"))
			output.PrintTable(headers, buildOperationRows(result))
		}
	}

	result, err := service.List(ctx, opts)
	if err != nil {
		return err
	}
	renderResults(result)

	if allOperationsSettled(result) {
		output.Printfln("All operations have reached terminal status")
		return nil
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			result, err := service.List(ctx, opts)
			if err != nil {
				return err
			}

			if len(result.Operations) == 0 {
				output.Printfln("No operations found")
				return nil
			}

			renderResults(result)

			if allOperationsSettled(result) {
				output.Printfln("All operations have reached terminal status")
				return nil
			}
		}
	}
}

func buildOperationRows(result *OperationsListResult) [][]string {
	rows := make([][]string, len(result.Operations))
	for i, op := range result.Operations {
		rows[i] = []string{
			fmt.Sprintf("%d", op.ID),
			op.OperationDisplayName,
			op.ProtocolDisplayName,
			formatOperationStatusWithColor(op.Status),
			op.CID,
			fmt.Sprintf("%.0f%%", op.ProgressPercent),
			op.StartedAt,
		}
	}
	return rows
}

func allOperationsSettled(result *OperationsListResult) bool {
	for _, op := range result.Operations {
		if !portalsdk.OperationStatus(op.Status).IsSettled() {
			return false
		}
	}
	return true
}

func watchOperation(ctx context.Context, service OperationsService, output Output, id int64) error {
	output.PrintFields(FieldGroup{
		Title:  fmt.Sprintf("Watching operation %d", id),
		Fields: []Field{{"Instructions", "Press Ctrl+C to stop"}},
	})

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus string

	for {
		select {
		case <-ctx.Done():
			output.Printfln("Watch stopped")
			return nil
		case <-ticker.C:
			op, err := service.Get(ctx, id)
			if err != nil {
				return err
			}

			if lastStatus != op.Status {
				fields := []Field{
					{"Status", formatOperationStatusWithColor(op.Status)},
					{"Progress", fmt.Sprintf("%.0f%%", op.ProgressPercent)},
				}
				if op.StatusMessage != "" {
					fields = append(fields, Field{"Message", op.StatusMessage})
				}
				output.PrintFields(FieldGroup{PadTop: 1, Fields: fields})
				lastStatus = op.Status
			}

			if portalsdk.OperationStatus(op.Status).IsSettled() {
				output.Printfln("Operation has reached terminal status: %s", op.Status)
				return renderOperationDetail(output, op)
			}
		}
	}
}

func renderOperationDetail(output Output, op *OperationDetail) error {
	headers := []string{"Property", "Value"}
	rows := [][]string{
		{"ID", fmt.Sprintf("%d", op.ID)},
		{"CID", op.CID},
		{"Status", op.StatusDisplayName},
		{"Operation", op.OperationDisplayName},
		{"Protocol", op.ProtocolDisplayName},
		{"Progress", fmt.Sprintf("%.0f%%", op.ProgressPercent)},
		{"Started", op.StartedAt},
		{"Updated", op.UpdatedAt},
	}

	if op.CurrentStep != nil && op.TotalSteps != nil {
		rows = append(rows, []string{"Step", fmt.Sprintf("%d / %d", *op.CurrentStep, *op.TotalSteps)})
	}

	if op.StatusMessage != "" {
		rows = append(rows, []string{"Message", op.StatusMessage})
	}
	if op.Error != "" {
		rows = append(rows, []string{"Error", op.Error})
	}

	output.PrintTable(headers, rows)
	return nil
}

func formatOperationStatusWithColor(status string) string {
	switch portalsdk.OperationStatus(status) {
	case portalsdk.OperationStatusCompleted:
		return pterm.FgGreen.Sprint(status)
	case portalsdk.OperationStatusPending, portalsdk.OperationStatusRunning:
		return pterm.FgYellow.Sprint(status)
	case portalsdk.OperationStatusFailed, portalsdk.OperationStatusError:
		return pterm.FgRed.Sprint(status)
	default:
		return status
	}
}
