package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

func newWebsitesSSLCommand() *cli.Command {
	return &cli.Command{
		Name:  "ssl",
		Usage: "Manage SSL certificates for websites",
		Description: `View SSL certificate status for your websites.

SSL operations include:
  - Check SSL certificate status for a domain
  - Monitor SSL status changes in real-time

Examples:
  pinner websites ssl status example.com
  pinner websites ssl status example.com --json
  pinner websites ssl status example.com --watch`,
		Commands: []*cli.Command{
			newWebsitesSSLStatusCommand(),
		},
	}
}

func newWebsitesSSLStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Get SSL certificate status for a website",
		Description: `Get SSL certificate status for a website domain.

This command retrieves the current SSL certificate status including:
  - Certificate status (active, pending, error, etc.)
  - Certificate issuance date
  - Last update timestamp
  - Any error messages

Examples:
  pinner websites ssl status example.com
  pinner websites ssl status example.com --json
  pinner websites ssl status example.com --watch`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "watch",
				Usage: "Watch for SSL status changes",
			},
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesSSLStatus(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func websitesSSLStatus(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	setupCtx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain is required")
	}

	domain := args.First()
	watch := cmd.Bool("watch")

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	if watch {
		return output.Watch(ctx,
			func(ctx context.Context) (any, error) {
				return websitesService.GetSSLStatus(ctx, domain)
			},
			func(data any) (string, []string, [][]string) {
				website := data.(*ipfs.WebsiteResponse)
				title := fmt.Sprintf("SSL Status for %s - Last updated: %s", website.Domain, time.Now().Format("15:04:05"))

				if website.Ssl == nil {
					return title + "\n  No SSL information available", nil, nil
				}

				headers := []string{"Field", "Value"}
				rows := [][]string{
					{"Status", website.Ssl.Status},
					{"Issued At", formatTimePtr(website.Ssl.IssuedAt)},
					{"Last Updated", formatTimePtr(website.Ssl.LastUpdatedAt)},
				}

				if website.Ssl.Error != nil && *website.Ssl.Error != "" {
					rows = append(rows, []string{"Error", *website.Ssl.Error})
				}

				return title, headers, rows
			},
		)
	}

	website, err := websitesService.GetSSLStatus(setupCtx, domain)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printfln("SSL Status for %s", website.Domain)

	if website.Ssl == nil {
		output.Printfln("  No SSL information available")
		return nil
	}

	headers := []string{"Field", "Value"}
	rows := [][]string{
		{"Status", website.Ssl.Status},
		{"Issued At", formatTimePtr(website.Ssl.IssuedAt)},
		{"Last Updated", formatTimePtr(website.Ssl.LastUpdatedAt)},
	}

	if website.Ssl.Error != nil && *website.Ssl.Error != "" {
		rows = append(rows, []string{"Error", *website.Ssl.Error})
	}

	output.PrintTable(headers, rows)

	return nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "N/A"
	}

	return t.Format("2006-01-02 15:04:05")
}
