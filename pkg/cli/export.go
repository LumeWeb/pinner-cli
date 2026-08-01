package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// ExportService defines the interface for meta export operations.
type ExportService interface {
	RequireAuthenticated() error
	ExportDAG(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error)
	ExportSiaObject(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error)
}

type exportService struct {
	ipfsServiceBase
	service ipfs.MetaService
	client  *ipfs.Client
}

// ExportServiceOption is a function that configures an exportService.
type ExportServiceOption func(*exportService)

// WithExportAuthToken sets an auth token override that takes precedence over config.
func WithExportAuthToken(token string) ExportServiceOption {
	return func(s *exportService) {
		withAuthToken(token)(&s.ipfsServiceBase)
	}
}

// WithExportClient sets a pre-configured ipfs.Client, bypassing the default ipfs.NewClient() call.
func WithExportClient(client *ipfs.Client) ExportServiceOption {
	return func(s *exportService) {
		s.client = client
	}
}

type ExportServiceFactory func(cfgMgr config.Manager, output Output, secure bool, opts ...ExportServiceOption) ExportService

func defaultExportServiceFactory(cfgMgr config.Manager, output Output, secure bool, opts ...ExportServiceOption) ExportService {
	return NewExportService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), opts...)
}

type exportServiceFactoryFunc func(cfgMgr config.Manager, output Output, secure bool, opts ...ExportServiceOption) ExportService

var exportServiceFactory exportServiceFactoryFunc = defaultExportServiceFactory

// newAuthenticatedExportService creates an ExportService with authentication.
func newAuthenticatedExportService(cfgMgr config.Manager, output Output, authToken string, secure bool) (ExportService, error) {
	var svcOpts []ExportServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithExportAuthToken(authToken))
	}
	exportSvc := exportServiceFactory(cfgMgr, output, secure, svcOpts...)
	if err := exportSvc.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return exportSvc, nil
}

func NewExportService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...ExportServiceOption) ExportService {
	authToken := cfgMgr.Config().AuthToken

	s := &exportService{
		ipfsServiceBase: ipfsServiceBase{
			cfgMgr:    cfgMgr,
			authToken: authToken,
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.Meta()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.getAuthToken())
		if err != nil {
			output.PrintError(err)
			s.service = nil
			return s
		}
		s.service = client.Meta()
	}
	return s
}

func (s *exportService) ExportDAG(ctx context.Context, cid string) (*ipfs.DAGExportResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ExportDAG(ctx, cid)
}

func (s *exportService) ExportSiaObject(ctx context.Context, cid string) (*ipfs.CIDExportResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ExportSiaObject(ctx, cid)
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

Use 'export sia-object' to get the storage details for a single CID — the data
key, slab layout, and sector references needed to fetch it from Sia.

Examples:
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export sia-object bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json`,
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
		Description: `Shows the complete block structure for a CID — every block, its size, and how
blocks link together — along with where each block is stored on the Sia network.

Each block includes its Sia storage key, encryption key, and sector references,
so you can retrieve and decrypt the data directly from Sia without the portal.

Examples:
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export dag bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json`,
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
		Description: `Shows the Sia storage details for a single CID — the data key, slab layout,
encryption key, and sector references needed to fetch and decrypt the content
directly from the Sia network.

Use this when you want to retrieve one specific CID's data from Sia without
going through the portal. Use 'export dag' instead if you need the full block
structure for a root CID.

Examples:
  pinner export sia-object bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner export sia bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json`,
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

	exportSvc, err := newAuthenticatedExportService(cfgMgr, output, authToken, secure)
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
			strconv.FormatUint(block.Size, 10),
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

	exportSvc, err := newAuthenticatedExportService(cfgMgr, output, authToken, secure)
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

	if result.SharedObject != nil {
		output.Printfln("")
		output.Printfln("Shared Object:")
		output.Printfln("  Data Key: %v", result.SharedObject.DataKey)
		output.Printfln("  Slabs: %d", len(result.SharedObject.Slabs))
		for i, slab := range result.SharedObject.Slabs {
			output.Printfln("    [%d] Version: %d, Min Shards: %d, Sectors: %d, Offset: %d, Length: %d",
				i, slab.Version, slab.MinShards, len(slab.Sectors), slab.Offset, slab.Length)
		}
	} else {
		output.Printfln("No Sia shared object available for this CID")
	}

	return nil
}
