package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

const (
	ipnsSchemePrefix = "ipns://"
	ethSuffix        = ".eth"
	ethLimoFmt       = "https://%s.eth.limo"
	ipfsGatewayFmt   = "https://%s.ipns.inbrowser.link"
)

func newPointCommand() *cli.Command {
	return &cli.Command{
		Name:     "point",
		Category: "Management",
		Usage:    "Point an onchain domain at IPFS content",
		Description: `Point an onchain/decentralized domain at IPFS content via IPNS.

The command is idempotent. If an IPNS key for this domain already exists, it
reuses the key and republishes the new CID.

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
The domain will no longer resolve to IPFS content.

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

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
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

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	return unpointWithServices(ctx, name, ipnsService, output)
}

func pointWithServices(ctx context.Context, name, cid string, ipnsService IPNSService, output Output) error {
	if name == "" {
		return fmt.Errorf("name is required (e.g., vitalik.eth)")
	}

	key, isNew, err := resolveOrCreateIPNSKey(ctx, ipnsService, name)
	if err != nil {
		return err
	}

	if _, err := ipnsService.Publish(ctx, cid, key.Name, nil); err != nil {
		return fmt.Errorf("failed to publish to IPNS: %w", err)
	}

	contenthash := ipnsSchemePrefix + key.IpnsName

	if output.IsJSON() {
		result := map[string]any{
			"name":        name,
			"cid":         cid,
			"ipns_name":   key.IpnsName,
			"contenthash": contenthash,
			"created":     isNew,
		}
		if verifyURL := resolveVerifyURL(name, key.IpnsName); verifyURL != "" {
			result["verify_url"] = verifyURL
		}
		return output.PrintJSON(result)
	}

	output.PrintFields(FieldGroup{Title: "IPNS key published", Fields: []Field{
		{"Name", name},
		{"CID", cid},
		{"IPNS Name", key.IpnsName},
		{"Contenthash", contenthash},
	}})

	nextFields := []Field{
		{"Set contenthash", contenthash},
	}
	if verifyURL := resolveVerifyURL(name, key.IpnsName); verifyURL != "" {
		nextFields = append(nextFields, Field{"Verify", verifyURL})
	}
	output.PrintFields(FieldGroup{Title: "Next steps", Fields: nextFields})

	return nil
}

func unpointWithServices(ctx context.Context, name string, ipnsService IPNSService, output Output) error {
	if name == "" {
		return fmt.Errorf("name is required (e.g., vitalik.eth)")
	}

	keys, err := ipnsService.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("failed to list IPNS keys: %w", err)
	}

	var found *ipfs.IPNSKeyResponse
	for _, k := range keys {
		if k.Name == name {
			found = &k
			break
		}
	}

	if found == nil {
		return fmt.Errorf("no IPNS key found for %q", name)
	}

	if err := ipnsService.DeleteKey(ctx, fmt.Sprintf("%d", found.Id)); err != nil {
		return fmt.Errorf("failed to delete IPNS key: %w", err)
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"name":      name,
			"ipns_name": found.IpnsName,
			"deleted":   true,
		})
	}

	output.PrintFields(FieldGroup{Title: "IPNS key removed", Fields: []Field{
		{"Name", name},
		{"IPNS Name", found.IpnsName},
	}})

	return nil
}

func resolveOrCreateIPNSKey(ctx context.Context, ipnsService IPNSService, name string) (*ipfs.IPNSKeyResponse, bool, error) {
	created, createErr := ipnsService.CreateKey(ctx, name, nil)
	if createErr == nil {
		return created, true, nil
	}

	keys, listErr := ipnsService.ListKeys(ctx)
	if listErr != nil {
		return nil, false, fmt.Errorf("failed to list IPNS keys: %w", listErr)
	}

	for _, k := range keys {
		if k.Name == name {
			return &k, false, nil
		}
	}

	return nil, false, fmt.Errorf("IPNS key %q not found and creation failed: %w", name, createErr)
}

func resolveVerifyURL(name, ipnsName string) string {
	if strings.HasSuffix(strings.ToLower(name), ethSuffix) {
		return fmt.Sprintf(ethLimoFmt, name[:len(name)-len(ethSuffix)])
	}
	return fmt.Sprintf(ipfsGatewayFmt, ipnsName)
}
