package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

func newIPNSCommand() *cli.Command {
	return &cli.Command{
		Name:     "ipns",
		Category: "Management",
		Usage:    "Manage IPNS (InterPlanetary Name System) keys and records",
		Description: `IPNS provides a mutable address scheme for IPFS content, allowing you to
publish content under a stable name that you can update to point to new CIDs.

IPNS operations include:
  - Managing IPNS keys (create, list, get, delete)
  - Publishing CIDs to IPNS names
  - Republishing IPNS records
  - Resolving IPNS names to their target CIDs

Key names and numeric IDs are interchangeable. For example:
  pinner ipns keys get my-key
is equivalent to:
  pinner ipns keys get 1

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get my-key
  pinner ipns keys delete my-key
  pinner ipns publish bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --key-name my-key
  pinner ipns republish my-key
  pinner ipns resolve k51qzi5uqu5djx...

This works on raw IPNS keys. To point an entire hosted *website* at an IPNS key (and have Pinner manage it), use 'websites enable-ipns' instead.`,
		Commands: []*cli.Command{
			newIPNSKeysCommand(),
			newIPNSPublishCommand(),
			newIPNSRepublishCommand(),
			newIPNSResolveCommand(),
		},
	}
}

func newIPNSKeysCommand() *cli.Command {
	return &cli.Command{
		Name:  "keys",
		Usage: "Manage IPNS keys",
		Description: `Manage your IPNS keys (create, list, get, delete). Keys are named identities you publish CIDs to, so the IPNS name re-points to new content. Key names and numeric IDs are interchangeable.

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get my-key
  pinner ipns keys delete my-key

This manages the keys themselves. To actually point a key at a CID use 'ipns publish'; to refresh its on-network record use 'ipns republish'.`,
		Commands: []*cli.Command{
			newIPNSKeysListCommand(),
			newIPNSKeysCreateCommand(),
			newIPNSKeysGetCommand(),
			newIPNSKeysDeleteCommand(),
		},
	}
}

func newIPNSKeysListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all IPNS keys",
		Description: `List all IPNS keys for the authenticated user.

Examples:
  pinner ipns keys list
  pinner ipns keys list --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsKeysList(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSKeysCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new IPNS key",
		Description: `Create a new IPNS key with the given name. Use the key to
publish content under a stable IPNS name.

Examples:
  pinner ipns keys create my-key
  pinner ipns keys create my-key --key <private-key>
  pinner ipns keys create my-key --json

Does NOT publish content (use 'ipns publish' after creating). --key is sensitive private-key material; handle it securely and do not share it.`,
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			mcp.SensitiveStringFlag(&cli.StringFlag{
				Name:  FlagKey,
				Usage: "Private key to import (optional, generates a new key if not provided)",
			}),
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsKeysCreate(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSKeysGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get details of a specific IPNS key",
		Description: `Get details of one IPNS key by its name or numeric ID. Returns the key's ID, name, IPNS name, peer ID, and the CID it currently points to. Read-only: does not modify the key.

Examples:
  pinner ipns keys get my-key
  pinner ipns keys get 1
  pinner ipns keys get my-key --json`,
		ArgsUsage: "<key-name-or-id>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsKeysGet(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSKeysDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete an IPNS key",
		Description: `Delete an IPNS key by its name or numeric ID. DESTRUCTIVE and irreversible. The key and the ability to publish or update its IPNS name are gone forever.

Does NOT delete the site that may be using it: if a website targets this key (see 'websites get'), point it elsewhere or recreate the key first. Returns a deletion confirmation.

Examples:
  pinner ipns keys delete my-key
  pinner ipns keys delete 1`,
		ArgsUsage: "<key-name-or-id>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsKeysDelete(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSPublishCommand() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "Publish a CID to an IPNS key",
		Description: `Publish a CID to an IPNS key, making it resolvable via the IPNS name.
After publishing, the IPNS name will resolve to the specified CID.

Specify the key by name or numeric ID.

Examples:
  pinner ipns publish bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --key-name my-key
  pinner ipns publish bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --key-name my-key --ttl 24h
  pinner ipns publish bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --key-name 1 --json

Publishing a new CID to the same key is how you update content under a stable name; to refresh without changing the target use 'ipns republish'. This publishes a raw key; to switch a hosted website onto an IPNS key use 'websites enable-ipns'.`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "key-name",
				Aliases:  []string{"k"},
				Usage:    "Name or ID of the IPNS key to publish to",
				Required: true,
			},
			&cli.StringFlag{
				Name:    "ttl",
				Aliases: []string{"t"},
				Usage:   "Time-to-live for the IPNS record (e.g., 24h, 7d)",
			},
			WaitFlag(),
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsPublish(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSRepublishCommand() *cli.Command {
	return &cli.Command{
		Name:  "republish",
		Usage: "Republish an IPNS record for a key",
		Description: `Republish the IPNS record for a specific key. This refreshes the record
on the network, extending its validity.

Specify the key by name or numeric ID.

Examples:
  pinner ipns republish my-key
  pinner ipns republish 1
  pinner ipns republish my-key --json

Use this to keep a published name alive. To change what the name points to, use 'ipns publish' with a new CID instead.`,
		ArgsUsage: "<key-name-or-id>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsRepublish(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newIPNSResolveCommand() *cli.Command {
	return &cli.Command{
		Name:  "resolve",
		Usage: "Resolve an IPNS name to its target CID",
		Description: `Resolve an IPNS name (e.g. k51qzi5uqu5djx...) to the CID it currently points to. Returns the resolved target CID. Read-only inspection; it does not publish, republish, or create anything.

Examples:
  pinner ipns resolve k51qzi5uqu5djx...
  pinner ipns resolve k51qzi5uqu5djx... --json`,
		ArgsUsage: "<name>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return ipnsResolve(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func resolveIPNSKeyArg(ctx context.Context, ipnsService IPNSService, arg string) (string, error) {
	return resolveIPNSKeyIDToString(ctx, ipnsService, arg)
}

func ipnsKeysList(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	keys, err := ipnsService.ListKeys(ctx)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		output.Printfln("No IPNS keys found")
		return nil
	}

	if output.IsJSON() {
		result := map[string]any{
			"count": len(keys),
			"keys":  keys,
		}
		return output.PrintJSON(result)
	}

	output.Printfln("Found %d IPNS key(s)", len(keys))

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CID", "CREATED"}
	rows := make([][]string, len(keys))
	for i, key := range keys {
		value := "-"
		if key.Value != nil {
			value = *key.Value
		}
		rows[i] = []string{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			value,
			key.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysCreate(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	name := cmd.String(FlagName)
	if name == "" {
		return fmt.Errorf("name is required")
	}

	var key *string
	keyValue := cmd.String(FlagKey)
	if keyValue != "" {
		key = &keyValue
	}

	createdKey, err := ipnsService.CreateKey(ctx, name, key)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(createdKey)
	}

	output.Printfln("Successfully created IPNS key")

	value := "-"
	if createdKey.Value != nil {
		value = *createdKey.Value
	}

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", createdKey.Id),
			createdKey.Name,
			createdKey.IpnsName,
			createdKey.PeerId,
			value,
			createdKey.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysGet(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()
	keyID, err := resolveIPNSKeyArg(ctx, ipnsService, keyArg)
	if err != nil {
		return err
	}

	key, err := ipnsService.GetKey(ctx, keyID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(key)
	}

	output.Printfln("IPNS Key Details")

	value := "-"
	if key.Value != nil {
		value = *key.Value
	}

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			value,
			key.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysDelete(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()
	keyID, err := resolveIPNSKeyArg(ctx, ipnsService, keyArg)
	if err != nil {
		return err
	}

	if err := ipnsService.DeleteKey(ctx, keyID); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("IPNS key %s deleted successfully", keyArg),
		}
		return output.PrintJSON(result)
	}

	output.Printfln("IPNS key %s deleted successfully", keyArg)

	return nil
}

func ipnsPublish(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("CID is required")
	}

	cid := args.First()

	keyName := cmd.String("key-name")
	if keyName == "" {
		return fmt.Errorf("key-name is required")
	}

	var ttl *string
	ttlValue := cmd.String("ttl")
	if ttlValue != "" {
		ttl = &ttlValue
	}

	response, err := ipnsService.Publish(ctx, cid, keyName, ttl)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printfln("Published CID %s to IPNS name %s", response.Value, response.Name)

	headers := []string{"NAME", "VALUE", "PUBLISHED", "SEQUENCE", "VALIDITY"}
	rows := [][]string{
		{
			response.Name,
			response.Value,
			response.Published.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", response.Sequence),
			response.Validity.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsRepublish(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key name or ID is required")
	}

	keyArg := args.First()

	response, err := ipnsService.Republish(ctx, keyArg)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printfln("Republished IPNS key %s: %s (%d record(s))", keyArg, response.Message, response.Count)

	return nil
}

func ipnsResolve(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newAuthenticatedIPNSService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("IPNS name is required")
	}

	ipnsName := args.First()

	response, err := ipnsService.Resolve(ctx, ipnsName)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printfln("IPNS name %s resolves to CID %s", response.Name, response.Value)

	headers := []string{"NAME", "CID", "SEQUENCE", "EXPIRED", "EXPIRES"}
	rows := [][]string{
		{
			response.Name,
			response.Value,
			fmt.Sprintf("%d", response.Sequence),
			fmt.Sprintf("%t", response.Expired),
			response.Expires.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func resolveIPNSKeyIDToString(ctx context.Context, ipnsService IPNSService, arg string) (string, error) {
	id, err := resolveIPNSKeyID(ctx, ipnsService, arg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}
