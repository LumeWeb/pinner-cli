package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

func newUnpinCommand() *cli.Command {
	return &cli.Command{
		Name:  "unpin",
		Usage: "Remove a pin",
		Description: `Remove a pin by its CID. Prompts for confirmation by default.
Multiple CIDs can be provided as arguments, read from a file using --file, or piped from stdin.

Examples:
  pinner unpin QmHash
  pinner unpin QmHash --confirm
  pinner unpin QmHash1 QmHash2 QmHash3 --confirm
  pinner unpin --file cids.txt --confirm
  pinner unpin --file cids.txt --confirm --parallel 5 --continue
  pinner unpin QmHash --dry-run`,
		ArgsUsage: "<cid...>",
		Flags: []cli.Flag{
			ConfirmFlag(),
			FileFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
		},
		Metadata: WithTutorial(5, "Unpin content", fmt.Sprintf("pinner unpin %s", abbreviateCID(TutorialCID))),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return unpin(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

// unpinCommandGetter defines the interface for getting unpin command flags.
type unpinCommandGetter interface {
	String(name string) string
	Int(name string) int
	Bool(name string) bool
	GetCID() string
}

func unpin(ctx context.Context, cmd unpinCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
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

	confirm := cmd.Bool(FlagConfirm)
	filePath := cmd.String(FlagFile)
	parallel := cmd.Int(FlagParallel)
	continueOn := cmd.Bool(FlagContinue)
	dryRun := cmd.Bool(FlagDryRun)

	var cids []string

	if isStdinPipe() {
		cids, err = readLinesFromStdin()
		if err != nil {
			return fmt.Errorf("failed to read CIDs from stdin: %w", err)
		}
	} else if filePath != "" {
		cids, err = readCIDsFromFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read CIDs from file: %w", err)
		}
	} else {
		cid := cmd.GetCID()
		if cid == "" {
			return fmt.Errorf("%w. Usage: pinner unpin <cid> or use --file <path>", ErrCIDRequired)
		}
		cids = strings.Fields(cid)
	}

	if len(cids) == 0 {
		return fmt.Errorf("no CIDs provided")
	}

	if dryRun {
		options := make(map[string]string)
		if parallel > 1 && len(cids) > 1 {
			options[DryRunOptionParallel] = fmt.Sprintf("%d", parallel)
		}
		if continueOn {
			options[DryRunOptionContinueOnError] = "yes"
		}
		if confirm {
			options[DryRunOptionConfirm] = "no (using --confirm)"
		} else {
			options[DryRunOptionConfirm] = "yes"
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "unpin operations",
			Endpoint:  cfgMgr.Config().GetIPFSEndpoint(),
			Items:     cids,
			ItemLabel: "CIDs to unpin",
			Options:   options,
		})
		return nil
	}

	if len(cids) == 1 {
		result, err := pinningService.Unpin(ctx, cids[0], confirm)
		if err != nil {
			return err
		}
		if confirm {
			output.Printf("Unpinned CID: %s", result.CID)
		}
		return nil
	}

	batchOpts := BatchOptions{
		Parallel:   parallel,
		ContinueOn: continueOn,
	}

	result, err := pinningService.UnpinBatch(ctx, cids, batchOpts)
	if err != nil {
		return err
	}

	output.Printf("Batch operation completed in %s", result.Duration)
	output.Printf("Total: %d | Succeeded: %d | Failed: %d | Skipped: %d",
		result.Total, len(result.Succeeded), len(result.Failed), len(result.Skipped))

	if len(result.Failed) > 0 {
		output.Printf("\nFailed operations:")
		for _, fail := range result.Failed {
			output.Printf("  - %s: %s", fail.CID, fail.Error)
		}
	}

	return nil
}
