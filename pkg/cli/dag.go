package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
)

// DAGService defines the interface for DAG operations.
type DAGService interface {
	RequireAuthenticated() error
	ResolveDAG(ctx context.Context, cid string) (*ipfs.DAGResponse, error)
}

type dagService struct {
	*ipfsServiceBase
	service ipfs.DAGService
	client  *ipfs.Client
}

// DAGServiceOption is a function that configures a dagService.
type DAGServiceOption func(*dagService)

// WithDAGAuthToken sets an auth token override that takes precedence over config.
func WithDAGAuthToken(token string) DAGServiceOption {
	return func(s *dagService) {
		withAuthToken(token)(s.ipfsServiceBase)
	}
}

// WithDAGClient sets a pre-configured ipfs.Client, bypassing the default ipfs.NewClient() call.
func WithDAGClient(client *ipfs.Client) DAGServiceOption {
	return func(s *dagService) {
		s.client = client
	}
}

type DAGServiceFactory func(cfgMgr config.Manager, output Output, secure bool, opts ...DAGServiceOption) DAGService

func defaultDAGServiceFactory(cfgMgr config.Manager, output Output, secure bool, opts ...DAGServiceOption) DAGService {
	return NewDAGService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), opts...)
}

type dagServiceFactoryFunc func(cfgMgr config.Manager, output Output, secure bool, opts ...DAGServiceOption) DAGService

var dagServiceFactory dagServiceFactoryFunc = defaultDAGServiceFactory

// newAuthenticatedDAGService creates a DAGService with authentication.
// It returns an error if the user is not authenticated.
func newAuthenticatedDAGService(cfgMgr config.Manager, output Output, authToken string, secure bool) (DAGService, error) {
	var svcOpts []DAGServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithDAGAuthToken(authToken))
	}
	dagSvc := dagServiceFactory(cfgMgr, output, secure, svcOpts...)
	if err := dagSvc.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return dagSvc, nil
}

func NewDAGService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...DAGServiceOption) DAGService {
	authToken := cfgMgr.Config().AuthToken

	s := &dagService{
		ipfsServiceBase: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken(authToken)),
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.DAG()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.GetAuthToken())
		if err != nil {
			output.PrintError(err)
			s.service = nil
			return s
		}
		s.service = client.DAG()
	}
	return s
}

func (s *dagService) ResolveDAG(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ResolveDAG(ctx, cid)
}

func newDagCommand() *cli.Command {
	return &cli.Command{
		Name:  "dag",
		Usage: "IPFS DAG operations",
		Description: `Resolve IPFS DAG (Directed Acyclic Graph) block structures.

Examples:
  pinner dag resolve bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner dag resolve bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json`,
		Commands: []*cli.Command{
			newDagResolveCommand(),
		},
	}
}

func newDagResolveCommand() *cli.Command {
	return &cli.Command{
		Name:      "resolve",
		Usage:     "Resolve the complete block graph (DAG) for a root CID",
		ArgsUsage: "<cid>",
		Description: `Resolves the complete block graph for a root CID in a single HTTP roundtrip.

Returns the root CID, total block count, and each block node with its CID, size, and children.

Examples:
  pinner dag resolve bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner dag resolve bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json

Returns the IPFS-side block graph only. Does NOT include Sia storage locations; for portal storage metadata (where blocks live on Sia) use 'export dag' instead. Requires authentication.`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return dagResolve(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func dagResolve(ctx context.Context, cmd commandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() < 1 {
		return fmt.Errorf("CID argument required")
	}
	cid := args.First()

	dagSvc, err := newAuthenticatedDAGService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	result, err := dagSvc.ResolveDAG(ctx, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Root CID: %s", result.RootCid)
	output.Printfln("Total blocks: %d", len(result.Nodes))

	if len(result.Nodes) == 0 {
		return nil
	}

	headers := []string{"CID", "SIZE", "CHILDREN"}
	rows := make([][]string, len(result.Nodes))
	for i, node := range result.Nodes {
		rows[i] = []string{
			node.Cid,
			strconv.Itoa(node.Size),
			strconv.Itoa(len(node.Children)),
		}
	}

	output.PrintTable(headers, rows)
	return nil
}
