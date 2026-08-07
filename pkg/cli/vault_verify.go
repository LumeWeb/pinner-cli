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
		ArgsUsage: vaultArgsUsageFile,
		Description: `Check a vault file's integrity: verifies its recorded SHA-256 digest matches and that the object exists on the Sia indexer. Returns an OK/FAIL result with digest and object facts.

Does NOT stream or return file content: use vault cat for content. For non-integrity metadata, use vault stat.`,

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
