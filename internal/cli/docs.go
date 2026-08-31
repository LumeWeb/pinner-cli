//go:build !no_tunnel

package cli

import (
	"context"
	"os"

	docs "github.com/LumeWeb/cli-docs"
	"github.com/urfave/cli/v3"
)

func newDocsCommand() *cli.Command {
	return &cli.Command{
		Name:     "generate-docs",
		Usage:    "Generate CLI documentation in markdown, man, or JSON format",
		Category: "System",
		Commands: []*cli.Command{
			{
				Name:  "markdown",
				Usage: "Generate markdown documentation",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "Output file (default: stdout)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					md, err := docs.ToMarkdown(NewRootCommand())
					if err != nil {
						return err
					}
					if out := cmd.String("output"); out != "" {
						return os.WriteFile(out, []byte(md), 0o644)
					}
					_, err = cmd.Root().Writer.Write([]byte(md))
					return err
				},
			},
			{
				Name:  "man",
				Usage: "Generate man page documentation",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "Output file (default: stdout)",
					},
					&cli.IntFlag{
						Name:  "section",
						Usage: "Man section number",
						Value: 1,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					man, err := docs.ToManWithSection(NewRootCommand(), cmd.Int("section"))
					if err != nil {
						return err
					}
					if out := cmd.String("output"); out != "" {
						return os.WriteFile(out, []byte(man), 0o644)
					}
					_, err = cmd.Root().Writer.Write([]byte(man))
					return err
				},
			},
			{
				Name:  "json",
				Usage: "Generate JSON documentation",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "Output file (default: stdout)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					j, err := docs.ToJSON(NewRootCommand())
					if err != nil {
						return err
					}
					if out := cmd.String("output"); out != "" {
						return os.WriteFile(out, j, 0o644)
					}
					_, err = cmd.Root().Writer.Write(j)
					return err
				},
			},
		},
	}
}
