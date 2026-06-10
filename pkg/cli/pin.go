package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newPinCommand() *cli.Command {
	return &cli.Command{
		Name:     "pin",
		Category: "Pinning",
		Usage:    "Pin existing content by CID (see: pinner pins add)",
		Description: `Pin content that is already on IPFS by providing its CID.
Multiple CIDs can be provided as arguments, read from a file using --file, or piped from stdin.

Examples:
  pinner pin QmHash
  pinner pin QmHash --name "my file"
  pinner pin QmHash --no-wait
  pinner pin QmHash1 QmHash2 QmHash3 --parallel 5
  pinner pin --file cids.txt
  echo "QmHash" | pinner pin
  pinner pin --file cids.txt --continue --parallel 10
  pinner pin QmHash --dry-run`,
		ArgsUsage: "<cid...>",
		Flags: []cli.Flag{
			NameFlag("Custom name for the pin"),
			NoWaitFlag(),
			WaitFlagHidden(),
			FileFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
		},
		Metadata: WithTutorial(2, "Pin by CID", fmt.Sprintf("pinner pin %s", abbreviateCID(TutorialCID))),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return pin(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

// pinCommandGetter defines the interface for getting pin command flags.
type pinCommandGetter interface {
	String(name string) string
	Int(name string) int
	Bool(name string) bool
	GetCID() string
}

func pin(ctx context.Context, cmd pinCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	var pinningService PinningService
	if c, ok := cmd.(*cliCommandWrapper); ok {
		secure := GetSecureSetting(c.Command, cfgMgr)
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
		} else {
			pinningService = pinningServiceFactory(cfgMgr, output)
		}
	} else {
		pinningService = pinningServiceFactory(cfgMgr, output)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	name := cmd.String(FlagName)
	wait := !cmd.Bool(FlagNoWait)
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
			return fmt.Errorf("cid is required or provide --file or pipe from stdin")
		}
		cids = strings.Fields(cid)
	}

	if len(cids) == 0 {
		return fmt.Errorf("no CIDs provided")
	}

	if dryRun {
		options := make(map[string]string)
		if name != "" {
			options[DryRunOptionName] = name
		}
		if !wait {
			options[DryRunOptionNoWait] = "yes"
		}
		if parallel > 1 && len(cids) > 1 {
			options[DryRunOptionParallel] = fmt.Sprintf("%d", parallel)
		}
		if continueOn {
			options[DryRunOptionContinueOnError] = "yes"
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "pinning operations",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointSecure(),
			Items:     cids,
			ItemLabel: "CIDs to pin",
			Options:   options,
		})
		return nil
	}

	if len(cids) == 1 {
		result, err := pinningService.Pin(ctx, cids[0], name, wait)
		if err != nil {
			return err
		}
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"CID", result.CID},
				{"Request ID", result.RequestID},
				{"Status", result.Status},
			},
		})
		return nil
	}

	batchOpts := BatchOptions{
		Parallel:   parallel,
		ContinueOn: continueOn,
		Wait:       wait,
		Progress:   true,
	}

	result, err := pinningService.PinBatch(ctx, cids, name, batchOpts)
	if err != nil {
		return err
	}

	output.PrintBatchResult(result)

	return nil
}

func defaultPinningServiceFactory(cfgMgr config.Manager, output Output) PinningService {
	return NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointSecure())
}
