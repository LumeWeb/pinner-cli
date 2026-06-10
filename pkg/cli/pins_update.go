package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newPinsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update pin name and metadata",
		Description: `Update name and/or metadata for a pin.

Examples:
  pinner pins update QmHash --name "renamed"
  pinner pins update QmHash --meta owner=alice --meta env=prod
  pinner pins update QmHash --clear-meta
  pinner pins update QmHash --clear-meta --meta fresh=start
  pinner pins update QmHash --name "renamed" --meta env=prod`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			NameFlag("Rename the pin"),
			MetaFlag(),
			ClearMetaFlag(),
			DryRunFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return pinsUpdate(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}

// pinsUpdateCommandGetter defines the interface for getting pins update command flags.
type pinsUpdateCommandGetter interface {
	String(name string) string
	StringSlice(name string) []string
	Bool(name string) bool
	IsSet(name string) bool
	GetCID() string
}

func pinsUpdate(ctx context.Context, cmd pinsUpdateCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, pinningServiceFactory PinningServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	var secure bool
	var pinningService PinningService
	if c, ok := cmd.(*cliCommandWrapper); ok {
		authToken := GetAuthToken(c.Command, cfgMgr)
		secure = GetSecureSetting(c.Command, cfgMgr)
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

	cid := cmd.GetCID()
	if cid == "" {
		return fmt.Errorf("cid is required")
	}

	if err := requireUpdateFields(cmd, FlagName, FlagMeta, FlagClearMeta); err != nil {
		return err
	}

	name := cmd.String(FlagName)
	metaPairs := cmd.StringSlice(FlagMeta)
	clearMeta := cmd.Bool(FlagClearMeta)
	dryRun := cmd.Bool(FlagDryRun)

	var parsedMeta map[string]string
	if len(metaPairs) > 0 {
		var err error
		parsedMeta, err = parseMetaPairs(metaPairs)
		if err != nil {
			return err
		}
	}

	if dryRun {
		options := make(map[string]string)
		options[DryRunOptionCID] = cid
		if name != "" {
			options["Name"] = name
		}
		if clearMeta {
			options["Clear metadata"] = "true"
		}
		if len(parsedMeta) > 0 {
			options["Metadata pairs"] = fmt.Sprintf("%d", len(parsedMeta))
			for k, v := range parsedMeta {
				options["  "+k] = v
			}
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "pin update",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointWithSecure(secure),
			Options:   options,
		})
		return nil
	}

	var metaSlice []string
	if len(parsedMeta) > 0 {
		metaSlice = metaMapToSlice(parsedMeta)
	}
	return pinningService.UpdatePin(ctx, cid, name, metaSlice, clearMeta)
}
