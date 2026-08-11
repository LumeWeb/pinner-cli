package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/export"
	meta "go.lumeweb.com/portal-sdk/meta"
)


// ExportService and options are re-exported from core; the impl lives in core/export.
type ExportService = export.Service
type ExportServiceOption = export.Option
type ExportServiceFactory = export.ServiceFactoryFunc

// WithExportAuthToken sets an auth token override (delegates to core).
func WithExportAuthToken(token string) ExportServiceOption {
	return export.WithAuthToken(token)
}

// WithExportMetaClient sets a pre-configured meta client (delegates to core).
func WithExportMetaClient(client *meta.MetaClient) ExportServiceOption {
	return export.WithMetaClient(client)
}

// newExportAPI builds the export service used by CLI handlers.
var newExportAPI = func(cfgMgr config.Manager, authToken string, secure bool) (ExportService, error) {
	return export.NewAuthenticated(cfgMgr, authToken, secure)
}


func newExportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "Export storage metadata for a CID",
		Description: `Export storage metadata for content pinned on the portal.

Use 'export dag' to get the full block structure of a CID along with where each
block is stored on the Sia network (data keys, encryption keys, sector locations).
This lets you retrieve and decrypt your data directly from Sia without going
through the portal.

Use 'export sia-object' to get the storage details for a single CID: the data
key, slab layout, and sector references needed to fetch it from Sia.

Examples:
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export sia-object bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json

Does NOT resolve the raw IPFS DAG graph (use 'dag resolve') and does NOT download file bytes (use 'download'). Requires authentication.`,
		Commands: []*cli.Command{
			newExportDAGCommand(),
			newExportSiaObjectCommand(),
		},
	}
}

func newExportDAGCommand() *cli.Command {
	return &cli.Command{
		Name:      "dag",
		Usage:     "Export the full block structure and Sia storage locations for a CID",
		ArgsUsage: "<cid>",
		Description: `Shows the complete block structure for a CID: every block, its size, and how
blocks link together, along with where each block is stored on the Sia network.

Each block includes its Sia storage key, encryption key, and sector references,
so you can retrieve and decrypt the data directly from Sia without the portal.

Examples:
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json

This is portal metadata. If you only need the generic IPFS block graph (no Sia locations), prefer 'dag resolve' instead. Requires authentication.`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return exportDAG(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newExportSiaObjectCommand() *cli.Command {
	return &cli.Command{
		Name:      "sia-object",
		Aliases:   []string{"sia"},
		Usage:     "Export the Sia storage details for a CID",
		ArgsUsage: "<cid>",
		Description: `Shows the Sia storage details for a single CID: the data key, slab layout,
encryption key, and sector references needed to fetch and decrypt the content
directly from the Sia network.

Use this when you want to retrieve one specific CID's data from Sia without
going through the portal. Use 'export dag' instead if you need the full block
structure for a root CID.

Examples:
  pinner export sia-object bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export sia bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json

Requires authentication. For the full block graph use 'export dag' instead.`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return exportSiaObject(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func exportDAG(ctx context.Context, cmd commandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() < 1 {
		return fmt.Errorf("CID argument required")
	}
	cid := args.First()

	exportSvc, err := newExportAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	result, err := exportSvc.ExportDAG(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Root CID: %s", result.RootCid)
	output.Printfln("Total blocks: %d", result.TotalBlocks)
	output.Printfln("Total size: %d bytes", result.TotalSizeBytes)

	if len(result.Blocks) == 0 {
		return nil
	}

	headers := []string{"CID", "SIZE", "LINKS", "SIA OBJECT"}
	rows := make([][]string, len(result.Blocks))
	for i, block := range result.Blocks {
		hasSia := "no"
		if block.SiaObject != nil {
			hasSia = "yes"
		}
		rows[i] = []string{
			block.Cid,
			strconv.Itoa(block.Size),
			strconv.Itoa(len(block.Links)),
			hasSia,
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

func exportSiaObject(ctx context.Context, cmd commandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() < 1 {
		return fmt.Errorf("CID argument required")
	}
	cid := args.First()

	exportSvc, err := newExportAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	result, err := exportSvc.ExportSiaObject(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("CID: %s", result.Cid)
	output.Printfln("Size: %d bytes", result.SizeBytes)
	output.Printfln("Created: %s", result.CreatedAt)
	output.Printfln("Updated: %s", result.UpdatedAt)

	so := result.SharedObject
	if len(so.Slabs) == 0 {
		output.Printfln("No Sia shared object available for this CID")
		return nil
	}

	output.Printfln("")
	output.Printfln("Shared Object:")
	output.Printfln("  Data Key: %v", so.DataKey)
	output.Printfln("  Slabs: %d", len(so.Slabs))
	for i, slab := range so.Slabs {
		output.Printfln("    [%d] Version: %d, Min Shards: %d, Sectors: %d, Offset: %d, Length: %d",
			i, slab.Version, slab.MinShards, len(slab.Sectors), slab.Offset, slab.Length)
	}

	return nil
}
