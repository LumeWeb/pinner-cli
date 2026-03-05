package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Get pin status for CID",
		Description: `Check the status of a pin to see if it has been completed.

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
  failed   - Pin failed to pin`,
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
			return status(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

// statusCommandGetter defines the interface for getting status command flags.
type statusCommandGetter interface {
	Bool(name string) bool
	GetCID() string
}

func status(ctx context.Context, cmd statusCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
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
		pinStatus, err := pinningService.Status(ctx, cids[0], watch)
		if err != nil {
			return err
		}

		headers := []string{"Property", "Value"}
		rows := [][]string{
			{"CID", pinStatus.CID},
			{"Status", pinStatus.Status},
			{"Created", pinStatus.Created},
		}

		if len(pinStatus.Delegates) > 0 {
			// Add delegates as list items
			output.PrintTable(headers, rows)
			output.Printf("\nDelegates:")
			output.PrintList(pinStatus.Delegates)
		} else {
			output.PrintTable(headers, rows)
		}

		return nil
	}

	output.Printf("Checking status for %d CID(s)", len(cids))

	headers := []string{"CID", "STATUS", "CREATED"}
	rows := make([][]string, 0, len(cids))

	for _, cid := range cids {
		pinStatus, err := pinningService.Status(ctx, cid, false)
		if err != nil {
			rows = append(rows, []string{cid, fmt.Sprintf("Error: %v", err), ""})
			continue
		}

		rows = append(rows, []string{pinStatus.CID, formatStatusWithColor(pinStatus.Status), pinStatus.Created})
	}

	output.PrintTable(headers, rows)
	return nil
}
