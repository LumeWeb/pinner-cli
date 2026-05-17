package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/manifoldco/promptui"
	"github.com/urfave/cli/v3"
)

func newUnpinAllCommand() *cli.Command {
	return &cli.Command{
		Name:  "all",
		Usage: "Remove all pins",
		Description: `Remove all pinned content. This is a destructive operation with safety guards.

This command requires two explicit confirmations:
1. The --confirm flag to acknowledge the destructive nature
2. An interactive prompt requiring you to type the exact number of pins

For non-interactive use (scripts, CI), use --yes to accept the safety prompt.
--confirm is always required regardless of --yes.

Examples:
  pinner unpin all --confirm
  pinner unpin all --confirm --status failed
  pinner unpin all --confirm --parallel 5 --continue
  pinner unpin all --confirm --dry-run
  pinner unpin all --confirm --yes
  pinner unpin all --confirm --status queued --dry-run`,
		Flags: []cli.Flag{
			ConfirmFlag(),
			StatusFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
			&cli.BoolFlag{
				Name:  FlagYes,
				Usage: "Accept the safety prompt non-interactively (requires --confirm)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return unpinAll(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

type unpinAllCommandGetter interface {
	String(name string) string
	Int(name string) int
	Bool(name string) bool
}

func unpinAll(ctx context.Context, cmd unpinAllCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
	confirm := cmd.Bool(FlagConfirm)
	if !confirm {
		output.Printfln("Use --confirm to unpin all pins. This is a destructive operation.")
		return nil
	}

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

	statusFilter := cmd.String(FlagStatus)
	parallel := cmd.Int(FlagParallel)
	continueOn := cmd.Bool(FlagContinue)
	dryRun := cmd.Bool(FlagDryRun)
	yes := cmd.Bool(FlagYes)

	pins, err := pinningService.List(ctx, "", 0, statusFilter)
	if err != nil {
		return err
	}

	if len(pins) == 0 {
		output.Printfln("No pins found")
		return nil
	}

	if dryRun {
		cids := make([]string, len(pins))
		for i, pin := range pins {
			cids[i] = pin.CID
		}
		options := map[string]string{
			DryRunOptionConfirm: "no (using --confirm)",
		}
		if statusFilter != "" {
			options["Status filter"] = statusFilter
		}
		if parallel > 1 {
			options[DryRunOptionParallel] = fmt.Sprintf("%d", parallel)
		}
		if continueOn {
			options[DryRunOptionContinueOnError] = "yes"
		}
		if yes {
			options["Safety prompt"] = "auto-accepted (--yes)"
		} else {
			options["Safety prompt"] = "required"
		}

		RenderDryRun(output, DryRunPreview{
			Operation: fmt.Sprintf("unpin-all (%d pins)", len(pins)),
			Endpoint:  cfgMgr.Config().GetIPFSEndpoint(),
			Items:     cids,
			ItemLabel: "CIDs to unpin",
			Options:   options,
		})
		return nil
	}

	if !yes {
		expected := strconv.Itoa(len(pins))
		prompt := promptui.Prompt{
			Label: fmt.Sprintf("Type %s to confirm unpinning all %d pins", expected, len(pins)),
			Validate: func(input string) error {
				if input != expected {
					return fmt.Errorf("must type %s to confirm", expected)
				}
				return nil
			},
		}
		result, err := prompt.Run()
		if err != nil {
			if err == promptui.ErrInterrupt {
				return ErrUnpinAllAborted
			}
			return fmt.Errorf("safety prompt failed: %w", err)
		}
		if result != expected {
			return ErrUnpinAllAborted
		}
	}

	output.Printfln("Unpinning %d pin(s)...", len(pins))

	batchOpts := BatchOptions{
		Parallel:   parallel,
		ContinueOn: continueOn,
		Progress:   true,
	}

	result, err := pinningService.UnpinAll(ctx, statusFilter, batchOpts)
	if err != nil {
		return err
	}

	output.PrintBatchResult(result)

	return nil
}
