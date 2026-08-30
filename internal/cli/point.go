package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func newPointCommand() *cli.Command {
	return &cli.Command{
		Name:     "point",
		Category: "Management",
		Usage:    "Point an onchain domain at IPFS content",
		Description: `Point an onchain/decentralized domain at IPFS content via IPNS.

The command is idempotent. If an IPNS key for this domain already exists, it
reuses the key and republishes the new CID. It prints the ENS contenthash value
to set onchain and a verify URL; setting the contenthash is an onchain
transaction the user signs from their own wallet/ENS manager (Pinner does not do
it).

Examples:
  pinner point vitalik.eth --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner point vitalik.eth --cid bafybeig...updated

Does NOT manage non-domain IPNS publishing (use 'ipns publish'); to remove the pointing use 'unpoint'.`,
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagCID,
				Aliases:  []string{"c"},
				Usage:    "CID to point the name at",
				Required: true,
			},
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return point(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newUnpointCommand() *cli.Command {
	return &cli.Command{
		Name:     "unpoint",
		Category: "Management",
		Usage:    "Remove an onchain domain pointing",
		Description: `Remove the IPNS key for an onchain/decentralized domain.
The domain will no longer resolve to IPFS content. Clearing the onchain
contenthash in the ENS resolver (an onchain transaction) is a separate step the
user performs.

Examples:
  pinner unpoint vitalik.eth`,
		ArgsUsage: "<name>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return unpoint(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func point(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("name is required (e.g., vitalik.eth)")
	}

	name := args.First()
	cid := cmd.String(FlagCID)

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	return pointWithServices(ctx, name, cid, ipnsService, output)
}

func unpoint(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("name is required (e.g., vitalik.eth)")
	}

	name := args.First()

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	return unpointWithServices(ctx, name, ipnsService, output)
}

// pointWithServices renders the shared point logic (catalogops.PointENS) through
// the CLI Output formatter. The business logic lives in catalogops so the CLI and
// the MCP ens_point operation share one implementation; this file only owns the
// presentation.
func pointWithServices(ctx context.Context, name, cid string, ipnsService IPNSService, output Output) error {
	res, err := catalogops.PointENS(ctx, ipnsService, name, cid)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"name":        res.Name,
			"cid":         res.CID,
			"ipns_name":   res.IPNSName,
			"contenthash": res.Contenthash,
			"created":     res.Created,
		}
		if res.VerifyURL != "" {
			result["verify_url"] = res.VerifyURL
		}
		return output.PrintJSON(result)
	}

	output.PrintFields(FieldGroup{Title: "IPNS key published", Fields: []Field{
		{"Name", res.Name},
		{"CID", res.CID},
		{"IPNS Name", res.IPNSName},
		{"Contenthash", res.Contenthash},
	}})

	nextFields := []Field{
		{"Set contenthash", res.Contenthash},
		{"Wallet note", "Set the contenthash onchain from your own wallet/ENS manager (e.g. app.ens.domains). Pinner cannot sign this for you."},
	}
	if res.VerifyURL != "" {
		nextFields = append(nextFields, Field{"Verify", res.VerifyURL})
	}
	output.PrintFields(FieldGroup{Title: "Next steps", Fields: nextFields})

	return nil
}

func unpointWithServices(ctx context.Context, name string, ipnsService IPNSService, output Output) error {
	res, err := catalogops.UnpointENS(ctx, ipnsService, name)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"name":      res.Name,
			"ipns_name": res.IPNSName,
			"deleted":   res.Deleted,
		})
	}

	output.PrintFields(FieldGroup{Title: "IPNS key removed", Fields: []Field{
		{"Name", res.Name},
		{"IPNS Name", res.IPNSName},
	}})

	output.PrintFields(FieldGroup{Title: "Next steps", Fields: []Field{
		{"Wallet note", "The IPNS key is removed. If the name should stop resolving, also clear/update the contenthash record in your ENS resolver (onchain, from your wallet/ENS manager)."},
	}})

	return nil
}

// resolveVerifyURL returns the gateway URL to confirm an onchain/ENS name
// resolves. It delegates to the shared catalogops helper so the CLI and MCP
// surfaces agree on the verify URL. Kept as a package function for the CLI
// tests.
func resolveVerifyURL(name, ipnsName string) string {
	return catalogops.ResolveVerifyURL(name, ipnsName)
}
