package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVaultVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:      "verify",
		Usage:     "Verify content integrity of a vault file",
		ArgsUsage: "vault:/path/to/file",
		Description: `Checks that the file's content digest matches and the object exists on the indexer.

Verifies:
  1. SHA-256 digest is recorded
  2. Object exists on the Sia indexer`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			result, err := svc.Verify(ctx, vaultPath)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(result)
				return nil
			}

			status := "FAIL"
			if result.DigestMatch && result.ObjectExists {
				status = "OK"
			}

			output.PrintFields(FieldGroup{
				Title: "Verification Result",
				Fields: []Field{
					{Label: "Path", Value: result.Path},
					{Label: "Status", Value: status},
					{Label: "Content Digest", Value: result.ContentDigest},
					{Label: "Digest Match", Value: fmt.Sprintf("%v", result.DigestMatch)},
					{Label: "Object Exists", Value: fmt.Sprintf("%v", result.ObjectExists)},
					{Label: "Object ID", Value: result.ObjectID},
				},
			})
			return nil
		},
	}
}
