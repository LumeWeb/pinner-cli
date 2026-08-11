package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func newAccountAPIKeysCommand() *cli.Command {
	// The api-keys parent is catalog-driven (see apikeys_wiring.go).
	return apiKeysParent()
}

func accountAPIKeysList(ctx context.Context, cmd flagGetter, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, apiEndpoint)
	svc := svcFactory(authService, authToken)

	search := cmd.String(FlagSearch)
	keys, total, err := svc.ListAPIKeys(ctx, search)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

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
	if currentUUID != "" && !isUUIDString(idOrName) {
		keys, _, listErr := svc.ListAPIKeys(ctx, idOrName)
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

	if err := svc.DeleteAPIKey(ctx, idOrName, force); err != nil {
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
