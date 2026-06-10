package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newStatusCommand() *cli.Command {
	return &cli.Command{
		Name:     "status",
		Category: "Pinning",
		Usage:    "Get pin status for CID (see: pinner pins status)",
		Description: `Check the status of a pin to see if it has been completed.
If the pin is not found, account operations are checked as a fallback.

Examples:
  pinner status QmHash
  pinner status QmHash --watch
  pinner status QmHash --json
  echo "QmHash" | pinner status
  cat cids.txt | pinner status

Status values:
  queued   - Pin is queued for processing
  pinning  - Pin is being processed
  pinned   - Pin is successfully pinned
  failed   - Pin failed to pin

Operation status values (shown when pin is not found):
  pending   - Operation is queued
  running   - Operation is in progress
  completed - Operation finished successfully
  failed    - Operation failed
  error     - Operation encountered an error`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "watch",
				Usage: "Poll until settled",
			},
		},
		Metadata: WithTutorial(4, "Check pin status", fmt.Sprintf("pinner status %s", abbreviateCID(TutorialCID))),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return status(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory, defaultStatusServiceFactory)
		},
	}
}

type statusCommandGetter interface {
	Bool(name string) bool
	GetCID() string
}

func defaultStatusServiceFactory(cfgMgr config.Manager, output Output, pinningService PinningService, authService AuthService) StatusService {
	return NewStatusService(cfgMgr, output, pinningService, authService)
}

func status(ctx context.Context, cmd statusCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory, statusServiceFactory StatusServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	var pinningService PinningService
	if c, ok := cmd.(*cliCommandWrapper); ok {
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpoint(), WithAuthToken(authToken))
		} else {
			pinningService = pinningServiceFactory(cfgMgr, output)
		}
	} else {
		pinningService = pinningServiceFactory(cfgMgr, output)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	cid := cmd.GetCID()
	if cid == "" {
		return fmt.Errorf("%w. Usage: pinner status <cid> or pipe CIDs from stdin", ErrCIDRequired)
	}

	watch := cmd.Bool("watch")

	authService := NewAuthService(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
	statusService := statusServiceFactory(cfgMgr, output, pinningService, authService)

	var cids []string
	if isStdinPipe() {
		var err error
		cids, err = readLinesFromStdin()
		if err != nil {
			return fmt.Errorf("failed to read CIDs from stdin: %w", err)
		}
		if len(cids) == 0 {
			return fmt.Errorf("no CIDs provided via stdin")
		}
	} else {
		cids = []string{cid}
	}

	if len(cids) == 1 {
		pinStatus, opStatus, err := statusService.Status(ctx, cids[0], watch)
		if err != nil {
			return err
		}

		if pinStatus != nil {
			return renderPinStatus(output, pinStatus)
		}

		return renderOperationStatus(output, opStatus)
	}

	output.Printfln("Checking status for %d CID(s)", len(cids))

	headers := []string{"CID", "STATUS", "SOURCE", "CREATED"}
	rows := make([][]string, 0, len(cids))

	for _, cid := range cids {
		pinStatus, opStatus, err := statusService.Status(ctx, cid, false)
		if err != nil {
			rows = append(rows, []string{cid, fmt.Sprintf("Error: %v", err), "", ""})
			continue
		}

		if pinStatus != nil {
			rows = append(rows, []string{pinStatus.CID, formatStatusWithColor(pinStatus.Status), "pin", pinStatus.Created})
		} else if opStatus != nil {
			rows = append(rows, []string{opStatus.CID, formatStatusWithColor(opStatus.Status), "operation", opStatus.StartedAt})
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

func renderPinStatus(output Output, pinStatus *PinStatus) error {
	headers := []string{"Property", "Value"}
	rows := [][]string{
		{"CID", pinStatus.CID},
		{"Status", pinStatus.Status},
		{"Created", pinStatus.Created},
	}

	if len(pinStatus.Delegates) > 0 {
		output.PrintTable(headers, rows)
		output.PrintListGroup(ListGroup{
			Title:  "Delegates:",
			Items:   pinStatus.Delegates,
			PadTop: 1,
		})
	} else {
		output.PrintTable(headers, rows)
	}

	return nil
}

func renderOperationStatus(output Output, op *OperationStatusResult) error {
	headers := []string{"Property", "Value"}
	rows := [][]string{
		{"CID", op.CID},
		{"Status", op.StatusDisplayName},
		{"Operation", op.OperationDisplayName},
		{"Protocol", op.ProtocolDisplayName},
		{"Progress", fmt.Sprintf("%.0f%%", op.ProgressPercent)},
		{"Started", op.StartedAt},
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
