package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newMetadataCommand() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "Update pin metadata",
		Description: `Update metadata for a pin by setting or clearing key-value pairs.

Examples:
  pinner metadata QmHash --set category=backup --set environment=prod
  pinner metadata QmHash --clear
  pinner metadata QmHash --set author=alice --set version=1.0 --set date=2024-01-15
  echo -e "category=backup\nenvironment=prod" | pinner metadata QmHash
  pinner metadata QmHash --set category=backup --dry-run`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  FlagSet,
				Usage: "Set metadata as key=value (repeatable)",
			},
			&cli.BoolFlag{
				Name:  FlagClear,
				Usage: "Clear all metadata",
			},
			DryRunFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return metadata(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

// metadataCommandGetter defines the interface for getting metadata command flags.
type metadataCommandGetter interface {
	StringSlice(name string) []string
	Bool(name string) bool
	IsSet(name string) bool
	GetCID() string
}

func metadata(ctx context.Context, cmd metadataCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
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
		return fmt.Errorf("cid is required")
	}

	if err := requireUpdateFields(cmd, FlagSet, FlagClear); err != nil {
		return err
	}

	set := cmd.StringSlice(FlagSet)
	_clear := cmd.Bool(FlagClear)
	dryRun := cmd.Bool(FlagDryRun)

	if isStdinPipe() && len(set) == 0 && !_clear {
		lines, err := readLinesFromStdin()
		if err != nil {
			return fmt.Errorf("failed to read metadata from stdin: %w", err)
		}
		set = lines
	}

	if dryRun {
		options := make(map[string]string)
		options[DryRunOptionCID] = cid
		if _clear {
			options[DryRunOptionAction] = "Clear all metadata"
		}
		if len(set) > 0 {
			options[DryRunOptionAction] = fmt.Sprintf("Set metadata (%d key-value pair(s))", len(set)/2)
			for i := 0; i < len(set); i += 2 {
				if i+1 < len(set) {
					options["Metadata "+set[i]] = set[i+1]
				}
			}
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "metadata update",
			Endpoint:  cfgMgr.Config().GetIPFSEndpoint(),
			Options:   options,
		})
		return nil
	}

	return pinningService.UpdateMetadata(ctx, cid, set, _clear)
}
