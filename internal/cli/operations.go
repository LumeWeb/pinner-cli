package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v3"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func newOperationsCommand() *cli.Command {
	// The operations parent is catalog-driven (see operations_wiring.go).
	return newOperationsCommandCatalog()
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

// watchOperation polls a single operation until it reaches a terminal
// (settled) status, printing status/progress/message updates as they change
// and finally rendering the full detail. It mirrors the historical hand-written
// `operations get --watch` loop: human-output only, driven by the typed Get
// through a fixed 2s ticker, exiting early on context cancellation.
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
	case portalsdk.OperationStatusPending, portalsdk.OperationStatusProcessing:
		return pterm.FgYellow.Sprint(status)
	case portalsdk.OperationStatusFailed, portalsdk.OperationStatusDuplicate:
		return pterm.FgRed.Sprint(status)
	default:
		return status
	}
}
