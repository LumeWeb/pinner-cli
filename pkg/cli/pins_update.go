package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
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
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			return pinsUpdate(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}

func pinsUpdate(ctx context.Context, cmd interface {
	cidGetter
	flagGetterWithIsSet
	StringSlice(name string) []string
}, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory) error {
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
