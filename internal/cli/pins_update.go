package cli

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

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
		pinningService = pinningServiceFactory(cfgMgr, secure)
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
