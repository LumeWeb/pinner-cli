package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/dag"
)


// DAGService and options are re-exported from core; the impl lives in core/dag.
type DAGService = dag.Service
type DAGServiceOption = dag.Option
type DAGServiceFactory = dag.ServiceFactoryFunc

// newDAGAPI builds the DAG service used by CLI handlers.
var newDAGAPI = func(cfgMgr config.Manager, authToken string, secure bool) (DAGService, error) {
	return dag.NewAuthenticated(cfgMgr, authToken, secure)
}

// WithDAGAuthToken sets an auth token override (delegates to core).
func WithDAGAuthToken(token string) DAGServiceOption {
	return dag.WithAuthToken(token)
}

// WithDAGClient sets a pre-configured ipfs.Client (delegates to core).
func WithDAGClient(client *ipfs.Client) DAGServiceOption {
	return dag.WithClient(client)
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

	dagSvc, err := newDAGAPI(cfgMgr, authToken, secure)
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
