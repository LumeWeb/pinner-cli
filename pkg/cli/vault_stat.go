package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVaultStatCommand() *cli.Command {
	return &cli.Command{
		Name:      "stat",
		Usage:     "Show file or directory metadata",
		ArgsUsage: vaultArgsUsageFile,
		Description: `Show metadata for a single vault path: type, size, media type, content digest, and object ID.

Returns metadata only and does NOT stream file content. To view content, use vault cat; to list a directory's entries, use vault ls.`,
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

			result, err := svc.Stat(ctx, vaultPath)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(result)
				return nil
			}

			output.PrintFields(FieldGroup{
				Title: "Vault File Info",
				Fields: []Field{
					{Label: "Type", Value: result.Type},
					{Label: "Name", Value: result.Name},
					{Label: "Path", Value: result.Path},
					{Label: "Size", Value: fmt.Sprintf("%d bytes", result.Size)},
					{Label: "Media Type", Value: result.MediaType},
					{Label: "Content Digest", Value: result.ContentDigest},
					{Label: "Object ID", Value: result.ObjectID},
					{Label: "Created", Value: result.CreatedAt},
					{Label: "Updated", Value: result.UpdatedAt},
				},
			})
			return nil
		},
	}
}
