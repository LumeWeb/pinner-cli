package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func newUnpinAllCommand() *cli.Command {
	return &cli.Command{
		Name:  "all",
		Usage: "Remove all pins",
		Description: `Remove all pinned content. This is a destructive operation with safety guards.

This command requires two explicit confirmations:
1. The --force flag to acknowledge the destructive nature
2. An interactive prompt requiring you to type the exact number of pins

For non-interactive use (scripts, CI), use --yes to accept the safety prompt.
--force is always required regardless of --yes.

Examples:
  pinner unpin all --force
  pinner unpin all --force --status failed
  pinner unpin all --force --parallel 5 --continue
  pinner unpin all --force --dry-run
  pinner unpin all --force --yes
  pinner unpin all --force --status queued --dry-run`,
		Flags: []cli.Flag{
			ForceFlag(),
			ConfirmFlag(),
			StatusFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
			&cli.BoolFlag{
				Name:  FlagYes,
				Usage: "Accept the safety prompt non-interactively (requires --force)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			prompter := &PTermConfirmPrompter{}
			return unpinAll(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory, prompter)
		},
	}
}

func unpinAll(ctx context.Context, cmd flagGetterWithInt, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory, prompter ConfirmPrompter) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetSyncTimeout())
	defer cancel()

	confirm := cmd.Bool(FlagForce) || cmd.Bool(FlagConfirm)
	if !confirm {
		output.Printfln("Use --force to unpin all pins. This is a destructive operation.")
		return nil
	}

	var pinningService PinningService
	if authToken != "" {
		pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
	} else {
		pinningService = pinningServiceFactory(cfgMgr, output, secure)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	statusFilter := cmd.String(FlagStatus)
	parallel := cmd.Int(FlagParallel)
	continueOn := cmd.Bool(FlagContinue)
	dryRun := cmd.Bool(FlagDryRun)
	yes := cmd.Bool(FlagForce) || cmd.Bool(FlagYes)

	pins, err := pinningService.List(ctx, "", 0, statusFilter)
	if err != nil {
		return err
	}

	if len(pins) == 0 {
		output.Printfln("No pins found")
		return nil
	}

	if dryRun {
		items := make([]string, len(pins))
		for i, pin := range pins {
			items[i] = pin.RequestID
		}
		options := map[string]string{
			DryRunOptionConfirm: "no (using --force)",
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
			Endpoint:  cfgMgr.Config().GetIPFSEndpointWithSecure(secure),
			Items:     items,
			ItemLabel: "Request IDs to unpin",
			Options:   options,
		})
		return nil
	}

	if !yes {
		expected := strconv.Itoa(len(pins))
		result, err := prompter.Confirm(
			fmt.Sprintf("Type %s to confirm unpinning all %d pins", expected, len(pins)),
			expected,
		)
		if err != nil {
			return ErrUnpinAllAborted
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
