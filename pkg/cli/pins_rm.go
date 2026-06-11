package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newPinsRmCommand() *cli.Command {
	return &cli.Command{
		Name:  "rm",
		Usage: "Remove a pin by CID (see: pinner unpin)",
		Description: `Remove a pin by its CID, or remove all pins with --all.

Examples:
  pinner pins rm QmHash
  pinner pins rm QmHash --confirm
  pinner pins rm QmHash1 QmHash2 QmHash3 --confirm
  pinner pins rm --file cids.txt --confirm
  pinner pins rm QmHash --dry-run
  pinner pins rm --all --force
  pinner pins rm --all --status failed --force`,
		ArgsUsage: "<cid...>",
		Flags: []cli.Flag{
			ForceFlag(),
			ConfirmFlag(),
			FileFlag(),
			ParallelFlag(),
			ContinueFlag(),
			DryRunFlag(),
			StatusFlag(),
			&cli.BoolFlag{
				Name:  FlagAll,
				Usage: "Remove all pins (requires --force)",
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
			if c.Bool(FlagAll) {
				prompter := &PTermConfirmPrompter{}
				return unpinAll(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory, prompter)
			}
			return unpin(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}
