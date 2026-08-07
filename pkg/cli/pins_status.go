package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newPinsStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Get pin status for CID (see: pinner status)",
		Description: `Check whether a pin has completed.
If the pin is not found, account operations are checked as a fallback.

Examples:
  pinner pins status bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner pins status bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --watch
  pinner pins status bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json

The canonical form; 'status' is its shortcut. Does NOT download or stream content ('download'/'cat') and does NOT report your login ('auth status').`,
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
