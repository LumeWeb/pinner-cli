package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

func newVaultLsCommand() *cli.Command {
	return &cli.Command{
		Name:      "ls",
		Usage:     "List files and directories in the vault",
		ArgsUsage: vaultArgsUsage,
		Description: `List files and directories at the given vault path, returning name, type, size, and created time for each.

If no path is provided, lists the root directory. Lists one level only (no recursion).
For a single file's full metadata (digest, object ID, media type), use vault stat.`,

		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				vaultPath = vault.VaultRoot
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			items, err := svc.List(ctx, vaultPath)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(items)
				return nil
			}

			if len(items) == 0 {
				output.Printfln("Vault is empty.")
				return nil
			}

			headers := []string{"Name", "Type", "Size", "Created"}
			var rows [][]string
			for _, item := range items {
				rows = append(rows, []string{
					item.Name,
					item.Type,
					fmt.Sprintf("%d", item.Size),
					item.CreatedAt,
				})
			}
			output.PrintTable(headers, rows)
			return nil
		},
	}
}
