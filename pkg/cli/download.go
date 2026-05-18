package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfave/cli/v3"
)

const (
	FlagOutput = "output"
)

func OutputFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    FlagOutput,
		Aliases: []string{"o"},
		Usage:   "Output file or directory path (defaults to CID as filename in current directory)",
	}
}

func newDownloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "download",
		Usage: "Download pinned content from IPFS to a file",
		Description: `Download content from IPFS by CID and save it to the local filesystem.
Supports CID paths (e.g., CID/path/to/file) to download a specific file from a directory.

For streaming content to stdout, use 'pinner cat' instead.
For listing directory contents, use 'pinner ls' instead.

Examples:
  pinner download QmHash
  pinner download QmHash/subdir/file.txt
  pinner download QmHash -o myfile.txt
  pinner download QmHash -o /path/to/dir/
  pinner download QmHash -o existing.txt --force
  pinner download QmHash --dry-run

The output includes:
  - CID: Content identifier of the downloaded file
  - Path: Local file path where content was saved
  - Size: File size in human-readable format
  - Time: Download duration`,
		ArgsUsage: "<cid>[/<path>]",
		Flags: []cli.Flag{
			OutputFlag(),
			ForceFlag(),
			DryRunFlag(),
		},
		Metadata: WithTutorial(7, "Download pinned content", fmt.Sprintf("pinner download %s", abbreviateCID(TutorialCID))),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return handleDownload(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultDownloadServiceFactory)
		},
	}
}

func newCatCommand() *cli.Command {
	return &cli.Command{
		Name:  "cat",
		Usage: "Stream IPFS content to stdout",
		Description: `Stream the contents of an IPFS CID to stdout.
Supports CID paths (e.g., CID/path/to/file) to cat a specific file from a directory.

Examples:
  pinner cat QmHash
  pinner cat QmHash/path/to/file.txt
  pinner cat QmHash > output.txt
  pinner cat QmHash | jq .
  pinner cat QmHash | gzip > data.gz

Note: This command outputs raw content to stdout.
Use --verbose or redirect stderr for progress info.`,
		ArgsUsage: "<cid>[/<path>]",
		Flags:     []cli.Flag{},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return handleCat(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultDownloadServiceFactory)
		},
	}
}

func newLsCommand() *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List contents of an IPFS directory",
		Description: `List the contents of a directory pinned on IPFS by CID.
Shows file names, types, and sizes for each entry.
Supports CID paths (e.g., CID/path/to/dir) to list nested directories.

If the path targets a file, shows file metadata instead of erroring.

Examples:
  pinner ls QmHash
  pinner ls QmHash --json
  pinner ls QmHash --limit 5
  pinner ls QmHash/subdir`,
		ArgsUsage: "<cid>[/<path>]",
		Flags: []cli.Flag{
			LimitFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return handleLs(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultDownloadServiceFactory)
		},
	}
}

type downloadCommandGetter interface {
	String(name string) string
	Int(name string) int
	Bool(name string) bool
	Args() cli.Args
}

func handleDownload(ctx context.Context, cmd downloadCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, downloadServiceFactory DownloadServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	authService := NewAuthService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointSecure())

	var svcOpts []DownloadServiceOption
	svcOpts = append(svcOpts, WithDownloadAuthService(authService))

	if c, ok := cmd.(*cliCommandWrapper); ok {
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			svcOpts = append(svcOpts, WithDownloadAuthToken(authToken))
		}
	}

	cidStr := cmd.Args().First()
	if cidStr == "" {
		return ErrCIDRequired
	}

	outputPath := cmd.String(FlagOutput)
	force := cmd.Bool(FlagForce)
	dryRun := cmd.Bool(FlagDryRun)

	if dryRun {
		options := make(map[string]string)
		options[DryRunOptionCID] = cidStr
		if outputPath != "" {
			options["Output path"] = outputPath
		}
		if force {
			options["Force overwrite"] = "yes"
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "download operation",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointSecure(),
			Options:   options,
		})
		return nil
	}

	downloadService := downloadServiceFactory(cfgMgr, output, svcOpts...)

	if err := downloadService.RequireAuthenticated(); err != nil {
		return err
	}

	result, err := downloadService.Download(ctx, cidStr, outputPath, force)
	if err != nil {
		return err
	}

	if result != nil {
		if output.IsJSON() {
			return output.PrintJSON(struct {
				CID        string `json:"cid"`
				Path       string `json:"path"`
				Size       int64  `json:"size"`
				GatewayURL string `json:"gateway_url"`
				Duration   string `json:"duration"`
			}{
				CID:        result.CID,
				Path:       result.Path,
				Size:       result.Size,
				GatewayURL: cfgMgr.Config().GetGatewayEndpointSecure() + result.CID,
				Duration:   result.Duration.Round(time.Millisecond).String(),
			})
		}

		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"CID", result.CID},
				{"Path", result.Path},
				{"Size", humanReadableSize(result.Size)},
				{"Gateway URL", cfgMgr.Config().GetGatewayEndpointSecure() + result.CID},
				{"Time", result.Duration.Round(time.Millisecond).String()},
			},
		})
	}

	return nil
}

func handleCat(ctx context.Context, cmd downloadCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, downloadServiceFactory DownloadServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	authService := NewAuthService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointSecure())

	var svcOpts []DownloadServiceOption
	svcOpts = append(svcOpts, WithDownloadAuthService(authService))

	if c, ok := cmd.(*cliCommandWrapper); ok {
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			svcOpts = append(svcOpts, WithDownloadAuthToken(authToken))
		}
	}

	cidStr := cmd.Args().First()
	if cidStr == "" {
		return ErrCIDRequired
	}

	downloadService := downloadServiceFactory(cfgMgr, output, svcOpts...)

	if err := downloadService.RequireAuthenticated(); err != nil {
		return err
	}

	reader, err := downloadService.Cat(ctx, cidStr)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	_, err = io.Copy(os.Stdout, reader)
	return err
}

func handleLs(ctx context.Context, cmd downloadCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, downloadServiceFactory DownloadServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	authService := NewAuthService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointSecure())

	var svcOpts []DownloadServiceOption
	svcOpts = append(svcOpts, WithDownloadAuthService(authService))

	if c, ok := cmd.(*cliCommandWrapper); ok {
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			svcOpts = append(svcOpts, WithDownloadAuthToken(authToken))
		}
	}

	cidStr := cmd.Args().First()
	if cidStr == "" {
		return ErrCIDRequired
	}

	limit := cmd.Int(FlagLimit)

	downloadService := downloadServiceFactory(cfgMgr, output, svcOpts...)

	if err := downloadService.RequireAuthenticated(); err != nil {
		return err
	}

	entries, err := downloadService.ListDirectory(ctx, cidStr)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		if output.IsJSON() {
			return output.PrintJSON(LsResult{CID: cidStr, Entries: []DirEntry{}})
		}
		output.Printfln("Directory is empty")
		return nil
	}

	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}

	if output.IsJSON() {
		return output.PrintJSON(LsResult{CID: cidStr, Count: len(entries), Entries: entries})
	}

	headers := []string{"NAME", "TYPE", "SIZE"}

	rows := make([][]string, len(entries))
	for i, entry := range entries {
		sizeStr := humanReadableSize(entry.Size)
		if entry.Size < 0 {
			sizeStr = "-"
		}
		rows[i] = []string{entry.Name, entry.Type, sizeStr}
	}

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"CID", cidStr},
			{"Entries", fmt.Sprintf("%d", len(entries))},
		},
	})

	output.PrintTable(headers, rows)

	return nil
}
