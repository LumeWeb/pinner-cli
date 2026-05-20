package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
)

func newIPNSCommand() *cli.Command {
	return &cli.Command{
		Name:  "ipns",
		Usage: "Manage IPNS (InterPlanetary Name System) keys and records",
		Description: `IPNS provides a mutable address scheme for IPFS content, allowing you to
publish content under a stable name that can be updated to point to new CIDs.

IPNS operations include:
  - Managing IPNS keys (create, list, get, delete)
  - Publishing CIDs to IPNS names
  - Republishing IPNS records
  - Resolving IPNS names to their target CIDs

Key names can be used interchangeably with numeric IDs. For example:
  pinner ipns keys get my-key
is equivalent to:
  pinner ipns keys get 1

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get my-key
  pinner ipns keys delete my-key
  pinner ipns publish QmHash --key-name my-key
  pinner ipns republish my-key
  pinner ipns resolve k51qzi5uqu5djx...`,
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
		Description: `Manage your IPNS keys. Keys are used to publish content under stable
IPNS names that can be updated to point to new CIDs.

Key names can be used interchangeably with numeric IDs.

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get my-key
  pinner ipns keys delete my-key`,
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
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsKeysList(ctx, cmd, output)
		},
	}
}

func newIPNSKeysCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new IPNS key",
		Description: `Create a new IPNS key with the given name. The key can be used to
publish content under a stable IPNS name.

Examples:
  pinner ipns keys create my-key
  pinner ipns keys create my-key --key <private-key>
  pinner ipns keys create my-key --json`,
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagKey,
				Usage: "Private key to import (optional, generates a new key if not provided)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsKeysCreate(ctx, cmd, output)
		},
	}
}

func newIPNSKeysGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get details of a specific IPNS key",
		Description: `Get details of a specific IPNS key by its name or ID.

Examples:
  pinner ipns keys get my-key
  pinner ipns keys get 1
  pinner ipns keys get my-key --json`,
		ArgsUsage: "<key-name-or-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsKeysGet(ctx, cmd, output)
		},
	}
}

func newIPNSKeysDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete an IPNS key",
		Description: `Delete an IPNS key by its name or ID. This operation is irreversible.

Examples:
  pinner ipns keys delete my-key
  pinner ipns keys delete 1`,
		ArgsUsage: "<key-name-or-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsKeysDelete(ctx, cmd, output)
		},
	}
}

func newIPNSPublishCommand() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "Publish a CID to an IPNS key",
		Description: `Publish a CID to an IPNS key, making it resolvable via the IPNS name.
After publishing, the IPNS name will resolve to the specified CID.

The key can be specified by name or numeric ID.

Examples:
  pinner ipns publish QmHash --key-name my-key
  pinner ipns publish QmHash --key-name my-key --ttl 24h
  pinner ipns publish QmHash --key-name 1 --json`,
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
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsPublish(ctx, cmd, output)
		},
	}
}

func newIPNSRepublishCommand() *cli.Command {
	return &cli.Command{
		Name:  "republish",
		Usage: "Republish an IPNS record for a key",
		Description: `Republish the IPNS record for a specific key. This refreshes the record
on the network, extending its validity.

The key can be specified by name or numeric ID.

Examples:
  pinner ipns republish my-key
  pinner ipns republish 1
  pinner ipns republish my-key --json`,
		ArgsUsage: "<key-name-or-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsRepublish(ctx, cmd, output)
		},
	}
}

func newIPNSResolveCommand() *cli.Command {
	return &cli.Command{
		Name:  "resolve",
		Usage: "Resolve an IPNS name to its target CID",
		Description: `Resolve an IPNS name to its target CID. This shows which content
the IPNS name currently points to.

Examples:
  pinner ipns resolve k51qzi5uqu5djx...
  pinner ipns resolve k51qzi5uqu5djx... --json`,
		ArgsUsage: "<name>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return ipnsResolve(ctx, cmd, output)
		},
	}
}

func initIPNSService(ctx context.Context, cmd *cli.Command, output Output) (context.Context, context.CancelFunc, IPNSService, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		cancel()
		return ctx, func() {}, nil, err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		cancel()
		return ctx, func() {}, nil, err
	}

	return ctx, cancel, ipnsService, nil
}

func resolveIPNSKeyArg(ctx context.Context, ipnsService IPNSService, arg string) (string, error) {
	return resolveIPNSKeyIDToString(ctx, ipnsService, arg)
}

func ipnsKeysList(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := make([][]string, len(keys))
	for i, key := range keys {
		rows[i] = []string{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			key.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysCreate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", createdKey.Id),
			createdKey.Name,
			createdKey.IpnsName,
			createdKey.PeerId,
			createdKey.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysGet(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

	headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", key.Id),
			key.Name,
			key.IpnsName,
			key.PeerId,
			key.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

func ipnsKeysDelete(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

func ipnsPublish(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

func ipnsRepublish(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

func ipnsResolve(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, ipnsService, err := initIPNSService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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
