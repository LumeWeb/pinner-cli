package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newPinsAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Pin existing content by CID (see: pinner pin)",
		Description: `Pin content that is already on IPFS by providing its CID.
Optionally set metadata key-value pairs at pin time using --meta.

Examples:
  pinner pins add QmHash
  pinner pins add QmHash --name "my file"
  pinner pins add QmHash --no-wait
  pinner pins add QmHash --meta owner=alice --meta env=prod
  pinner pins add QmHash1 QmHash2 QmHash3 --parallel 5
  pinner pins add --file cids.txt
  pinner pins add QmHash --dry-run`,
		ArgsUsage: "<cid...>",
		Flags: []cli.Flag{
			NameFlag("Custom name for the pin"),
			NoWaitFlag(),
			WaitFlagHidden(),
			FileFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
			MetaFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			return pinsAdd(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}

func pinsAdd(ctx context.Context, cmd interface {
	cidGetter
	flagGetterWithIsSet
	StringSlice(name string) []string
}, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory) error {
	cids, err := pin(ctx, cmd, output, cfgMgr, authToken, secure, pinningServiceFactory)
	if err != nil {
		return err
	}

	if cmd.Bool(FlagDryRun) {
		return nil
	}

	metaPairs := cmd.StringSlice(FlagMeta)
	if len(metaPairs) == 0 {
		return nil
	}

	if len(cids) == 0 {
		return nil
	}

	meta, err := parseMetaPairs(metaPairs)
	if err != nil {
		return err
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

	slice := metaMapToSlice(meta)
	var lastErr error
	for _, cid := range cids {
		if err := pinningService.UpdateMetadata(ctx, cid, slice, false); err != nil {
			lastErr = fmt.Errorf("pin succeeded but metadata update failed for CID %s: %w", cid, err)
		}
	}
	return lastErr
}
