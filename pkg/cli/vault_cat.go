package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func newVaultCatCommand() *cli.Command {
	return &cli.Command{
		Name:      "cat",
		Usage:     "Stream file content to stdout",
		ArgsUsage: "vault:/path/to/file",
		Description: `Streams file content directly to stdout.
Progress and metadata go to stderr.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			cfgMgr, err := configManagerFactory()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetUploadTimeout())
			defer cancel()

			// Stream directly to stdout — data goes to stdout, progress to stderr
			if err := svc.Cat(ctx, vaultPath, os.Stdout); err != nil {
				return err
			}
			return nil
		},
	}
}
