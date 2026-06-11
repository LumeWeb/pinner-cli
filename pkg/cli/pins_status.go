package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newPinsStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Get pin status for CID (see: pinner status)",
		Description: `Check the status of a pin to see if it has been completed.
If the pin is not found, account operations are checked as a fallback.

Examples:
  pinner pins status QmHash
  pinner pins status QmHash --watch
  pinner pins status QmHash --json`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "watch",
				Usage: "Poll until settled",
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
			return status(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory, defaultStatusServiceFactory)
		},
	}
}
