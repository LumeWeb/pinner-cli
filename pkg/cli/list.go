package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:     "list",
		Category: "Pinning",
		Usage:    "List pinned content (see: pinner pins ls)",
		Description: `Shortcut for 'pinner pins ls'. List your pinned content with optional filtering.

Examples:
  pinner list
  pinner list --name "my-project"
  pinner list --status pinned
  pinner list --limit 20
  pinner list --watch
  pinner list --name backup --status failed --limit 50
  echo "backup" | pinner list`,
		ArgsUsage: "",
		Flags: []cli.Flag{
			NameFlag("Filter by name"),
			LimitFlag(),
			StatusFlag(),
			WatchFlag(),
		},
		Metadata: WithTutorial(3, "List all pins", "pinner list --name my-pin"),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			return list(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}

func list(ctx context.Context, cmd flagGetterWithInt, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory) error {
	setupCtx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	var pinningService PinningService
	if authToken != "" {
		pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
	} else {
		pinningService = pinningServiceFactory(cfgMgr, output, secure)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	nameFilter := cmd.String(FlagName)
	limit := cmd.Int(FlagLimit)
	statusFilter := cmd.String(FlagStatus)
	watch := cmd.Bool(FlagWatch)

	if isStdinPipe() && nameFilter == "" {
		patterns, err := readLinesFromStdin()
		if err != nil {
			return fmt.Errorf("failed to read patterns from stdin: %w", err)
		}
		if len(patterns) > 0 {
			nameFilter = patterns[0]
		}
	}

	if watch {
		return output.Watch(ctx,
			func(ctx context.Context) (any, error) {
				return pinningService.List(ctx, nameFilter, limit, statusFilter)
			},
			func(data any) (string, []string, [][]string) {
				pins := data.([]Pin)
				title := fmt.Sprintf("Found %d pin(s) - Last updated: %s", len(pins), time.Now().Format("15:04:05"))

				headers := []string{"CID", "NAME", "STATUS", "CREATED"}
				rows := make([][]string, len(pins))
				for i, pin := range pins {
					rows[i] = []string{pin.CID, pin.Name, formatStatusWithColor(pin.Status), pin.Created}
				}

				return title, headers, rows
			},
		)
	}

	pins, err := pinningService.List(setupCtx, nameFilter, limit, statusFilter)
	if err != nil {
		return err
	}

	if len(pins) == 0 {
		output.Printfln("No pins found")
		return nil
	}

	output.Printfln("Found %d pin(s)", len(pins))

	headers := []string{"CID", "NAME", "STATUS", "CREATED"}
	rows := make([][]string, len(pins))
	for i, pin := range pins {
		rows[i] = []string{pin.CID, pin.Name, formatStatusWithColor(pin.Status), pin.Created}
	}
	output.PrintTable(headers, rows)

	return nil
}
