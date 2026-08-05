package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVaultLsCommand() *cli.Command {
	return &cli.Command{
		Name:      "ls",
		Usage:     "List files and directories in the vault",
		ArgsUsage: "vault:/path",
		Description: `List files and directories at the given vault path.

If no path is provided, lists the root directory.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				vaultPath = "vault:/"
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
