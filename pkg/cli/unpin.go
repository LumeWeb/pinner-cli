package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newUnpinCommand() *cli.Command {
	return &cli.Command{
		Name:     "unpin",
		Category: "Pinning",
		Usage:    "Remove pins (see: pinner pins rm)",
		Description: `Shortcut for 'pinner pins rm'. Remove pins by CID or remove all pins.

Examples:
  pinner unpin bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner unpin bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --force
  pinner unpin bafybeig...abc bafybeig...def bafybeig...ghi --force
  pinner unpin --file cids.txt --force
  pinner unpin --file cids.txt --force --parallel 5 --continue
  pinner unpin bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --dry-run
  pinner unpin all --force
  pinner unpin all --force --status failed --dry-run`,
		ArgsUsage: "<cid...>",
		Flags: []cli.Flag{
			ForceFlag(),
			ConfirmFlag(),
			FileFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
		},
		Commands: []*cli.Command{
			newUnpinAllCommand(),
		},
		Metadata: WithTutorial(5, "Unpin content", fmt.Sprintf("pinner unpin %s", abbreviateCID(TutorialCID))),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			return unpin(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}

func unpin(ctx context.Context, cmd cidFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	var pinningService PinningService
	if authToken != "" {
		pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
	} else {
		pinningService = pinningServiceFactory(cfgMgr, output, secure)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	confirm := cmd.Bool(FlagForce) || cmd.Bool(FlagConfirm)
	filePath := cmd.String(FlagFile)
	parallel := cmd.Int(FlagParallel)
	continueOn := cmd.Bool(FlagContinue)
	dryRun := cmd.Bool(FlagDryRun)

	var cids []string
	var err error

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
			options[DryRunOptionConfirm] = "no (using --force)"
		} else {
			options[DryRunOptionConfirm] = "yes"
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "unpin operations",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointWithSecure(secure),
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
			output.Printfln("Unpinned CID: %s", result.CID)
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

	output.PrintBatchResult(result)

	return nil
}
