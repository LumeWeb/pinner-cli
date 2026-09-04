package cli

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

func websitesSSLStatus(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	setupCtx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain is required")
	}

	domain := args.First()
	watch := cmd.Bool("watch")

	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
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
					{"Status", string(website.Ssl.Status)},
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
		{"Status", string(website.Ssl.Status)},
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
