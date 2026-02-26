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
  - Resolving IPNS names to their target CIDs

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get k51qzi5uqu5djx...
  pinner ipns keys delete k51qzi5uqu5djx...
  pinner ipns publish QmHash --key-id 123
  pinner ipns resolve k51qzi5uqu5djx...`,
		Commands: []*cli.Command{
			newIPNSKeysCommand(),
			newIPNSPublishCommand(),
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

Examples:
  pinner ipns keys list
  pinner ipns keys create my-key
  pinner ipns keys get k51qzi5uqu5djx...
  pinner ipns keys delete k51qzi5uqu5djx...`,
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
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
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
			NameFlag("Custom name for the key"),
			&cli.StringFlag{
				Name:  "key",
				Usage: "Private key to import (optional, generates a new key if not provided)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
			return ipnsKeysCreate(ctx, cmd, output)
		},
	}
}

func newIPNSKeysGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get details of a specific IPNS key",
		Description: `Get details of a specific IPNS key by its ID.

Examples:
  pinner ipns keys get k51qzi5uqu5djx...
  pinner ipns keys get k51qzi5uqu5djx... --json`,
		ArgsUsage: "<key-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
			return ipnsKeysGet(ctx, cmd, output)
		},
	}
}

func newIPNSKeysDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete an IPNS key",
		Description: `Delete an IPNS key by its ID. This operation is irreversible.

Examples:
  pinner ipns keys delete k51qzi5uqu5djx...`,
		ArgsUsage: "<key-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
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

Examples:
  pinner ipns publish QmHash --key-id 123
  pinner ipns publish QmHash --key-id 123 --ttl 24h
  pinner ipns publish QmHash --key-id 123 --json`,
		ArgsUsage: "<cid>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "key-id",
				Aliases:  []string{"k"},
				Usage:    "ID of the IPNS key to publish to",
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
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
			return ipnsPublish(ctx, cmd, output)
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
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
			return ipnsResolve(ctx, cmd, output)
		},
	}
}

// Stub handlers for IPNS commands - to be implemented in subsequent tasks

func ipnsKeysList(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	keys, err := ipnsService.ListKeys(ctx)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		output.Printf("No IPNS keys found")
		return nil
	}

	if output.IsJSON() {
		result := map[string]any{
			"count": len(keys),
			"keys":  keys,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d IPNS key(s)", len(keys))

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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	name := cmd.String(FlagName)
	if name == "" {
		return fmt.Errorf("name is required")
	}

	var key *string
	keyValue := cmd.String("key")
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

	output.Printf("Successfully created IPNS key")

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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key ID is required")
	}

	keyID := args.First()

	key, err := ipnsService.GetKey(ctx, keyID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(key)
	}

	output.Printf("IPNS Key Details")

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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("key ID is required")
	}

	keyID := args.First()

	if err := ipnsService.DeleteKey(ctx, keyID); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("IPNS key %s deleted successfully", keyID),
		}
		return output.PrintJSON(result)
	}

	output.Printf("IPNS key %s deleted successfully", keyID)

	return nil
}

func ipnsPublish(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("CID is required")
	}

	cid := args.First()

	keyID := cmd.Int("key-id")
	if keyID == 0 {
		return fmt.Errorf("key-id is required")
	}

	var ttl *string
	ttlValue := cmd.String("ttl")
	if ttlValue != "" {
		ttl = &ttlValue
	}

	response, err := ipnsService.Publish(ctx, cid, keyID, ttl)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(response)
	}

	output.Printf("Published CID %s to IPNS name %s", response.Value, response.Name)

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

func ipnsResolve(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var ipnsService IPNSService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		ipnsService = NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		ipnsService = defaultIPNSServiceFactory(cfgMgr, output)
	}

	if err := ipnsService.RequireAuthenticated(); err != nil {
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

	output.Printf("IPNS name %s resolves to CID %s", response.Name, response.Value)

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
