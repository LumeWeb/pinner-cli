package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

func newVaultProfileUseCommand() *cli.Command {
	return &cli.Command{
		Name:      "use",
		Usage:     "Set the default profile for pinner vault commands",
		ArgsUsage: "<name>",
		Description: `Sets the profile used by default when neither --profile nor
the PINNER_PROFILE env var selects one.

After setting a default, commands like 'pinner vault status' or 'pinner vault cp'
resolve to this profile without needing --profile on each call. Note that an
explicit --profile flag or the PINNER_PROFILE env var still take precedence
over the configured default.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			name := c.Args().First()
			if name == "" {
				return fmt.Errorf("usage: pinner vault profile use <name>")
			}
			if err := vault.ValidateProfileName(name); err != nil {
				return err
			}

			if err := vault.SetDefaultProfile(name); err != nil {
				return err
			}
			output.Printfln("Default vault profile set to %q.", name)
			return nil
		},
	}
}
