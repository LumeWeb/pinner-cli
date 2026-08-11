package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

func newIPNSCommand() *cli.Command {
	// The ipns parent is catalog-driven: the keys + publish/republish/resolve
	// subcommands are compiled from the canonical operation catalog
	// (internal/catalogops) — see ipns_wiring.go.
	return newIPNSCommandCatalog()
}

func resolveIPNSKeyArg(ctx context.Context, ipnsService IPNSService, arg string) (string, error) {
	return resolveIPNSKeyIDToString(ctx, ipnsService, arg)
}

func ipnsKeysList(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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

	ipnsService, err := newIPNSAPI(cfgMgr, authToken, secure)
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
	id, err := ipns.ResolveKeyID(ctx, ipnsService, arg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}
