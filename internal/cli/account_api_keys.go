package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner/core/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func newAccountAPIKeysCommand() *cli.Command {
	// The api-keys parent is catalog-driven (see apikeys_wiring.go).
	return newAPIKeysCommand()
}

func accountAPIKeysList(ctx context.Context, cmd flagGetter, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, apiEndpoint)
	svc := svcFactory(authService, authToken)

	search := cmd.String(FlagSearch)
	keys, err := allAPIKeys(ctx, svc, search)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}
	total := len(keys)

	if len(keys) == 0 {
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{
				"count": 0,
				"keys":  []*portalsdk.APIKey{},
			})
		}
		output.Printfln("No API keys found")
		return nil
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"count": total,
			"keys":  keys,
		})
	}

	output.Printfln("Found %d API key(s)", total)

	headers := []string{"UUID", "NAME"}
	rows := make([][]string, len(keys))
	for i, key := range keys {
		rows[i] = []string{
			key.Uuid.String(),
			key.Name,
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

// allAPIKeys pages through every API key for the authenticated account. The
// backend applies a default 10-row window to ListAPIKeys unless an explicit
// _start/_end window is supplied, so a key that lives past the first page would
// otherwise be missed. This mirrors the allPlatformDomains/allSocialProviders
// full-scan helpers used for the admin name/id resolution paths.
func allAPIKeys(ctx context.Context, svc APIKeyService, search string) ([]*portalsdk.APIKey, error) {
	const pageSize = 100
	var all []*portalsdk.APIKey
	for start := 0; ; start += pageSize {
		keys, total, err := svc.ListAPIKeys(ctx, search, start, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, keys...)
		if len(keys) == 0 || len(keys) < pageSize || (total > 0 && len(all) >= total) {
			break
		}
	}
	return all, nil
}

func accountAPIKeysCreate(ctx context.Context, cmd argsFlagGetter, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, apiEndpoint)
	svc := svcFactory(authService, authToken)

	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("API key name is required")
	}

	apiKey, err := svc.CreateAPIKey(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"name":  apiKey.Name,
			"uuid":  apiKey.Uuid.String(),
			"token": apiKey.Token,
		})
	}

	output.Print("API key created successfully!")
	output.Printfln("Name: %s", apiKey.Name)
	output.Printfln("UUID: %s", apiKey.Uuid.String())
	output.Print("")
	output.Printfln("Token: %s", apiKey.Token)
	output.Print("")
	output.Print("Save this token securely; it cannot be retrieved later.")
	output.Print("Use it with: pinner auth --auth-token <token>")

	return nil
}

func accountAPIKeysDelete(ctx context.Context, cmd argsFlagGetterWithBool, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, apiEndpoint)
	svc := svcFactory(authService, authToken)

	idOrName := cmd.Args().First()
	if idOrName == "" {
		return fmt.Errorf("API key UUID or name is required")
	}

	force := cmd.Bool(FlagForce)

	currentUUID := svc.GetCurrentAPIKeyUUID()
	resolvedID := idOrName
	if !isUUIDString(idOrName) {
		// Resolve a name to its UUID using the name as the backend search
		// filter, while still paging through the (filtered) results so keys
		// past the backend's default first page are found. Passing the name as
		// the filter keeps the backend from returning every key on the account;
		// the paginated scan in allAPIKeys guarantees a match deeper than the
		// first page still resolves before we delete by the resolved UUID.
		keys, listErr := allAPIKeys(ctx, svc, idOrName)
		if listErr == nil {
			for _, key := range keys {
				if key.Name == idOrName {
					resolvedID = key.Uuid.String()
					break
				}
			}
		}
	}
	isCurrentKey := currentUUID != "" && currentUUID == resolvedID

	if err := svc.DeleteAPIKey(ctx, resolvedID, force); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"deleted": true,
			"key":     idOrName,
		}
		if isCurrentKey {
			result["warning"] = "You have deleted your current authentication key. Run 'pinner auth' to re-authenticate."
		}
		return output.PrintJSON(result)
	}

	output.Printfln("API key %q deleted", idOrName)
	if isCurrentKey {
		output.Print("")
		output.Print("WARNING: You have deleted your current authentication key.")
		output.Print("Run 'pinner auth' to re-authenticate.")
	}

	return nil
}
