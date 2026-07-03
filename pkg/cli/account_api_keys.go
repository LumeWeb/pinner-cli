package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func newAccountAPIKeysCommand() *cli.Command {
	return &cli.Command{
		Name:    "api-keys",
		Aliases: []string{"apikey", "api-key"},
		Usage:   "Manage API keys",
		Description: `Manage API keys for your Pinner.xyz account.

API keys are used to authenticate with the Pinner.xyz service without
exposing your login credentials. Each key has a unique token that can
be used with --auth-token or the PINNER_AUTH_TOKEN environment variable.

Examples:
  pinner account api-keys list
  pinner account api-keys list --search my-key
  pinner account api-keys create my-key
  pinner account api-keys delete my-key
  pinner account api-keys delete 00000000-0000-0000-0000-000000000001`,
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List API keys",
				Description: `List all API keys for your account.

Use --search to filter keys by name.`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    FlagSearch,
						Aliases: []string{"s"},
						Usage:   "Search API keys by name",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					output := setupOutput(cmd)
					cfgMgr, err := defaultConfigManagerFactory()
					if err != nil {
						return err
					}
					authToken := GetAuthToken(cmd, cfgMgr)
					return accountAPIKeysList(ctx, newCLICommandWrapper(cmd), output, cfgMgr, authToken, defaultAuthServiceFactory, defaultAPIKeyServiceFactory)
				},
			},
			{
				Name:      "create",
				Usage:     "Create a new API key",
				UsageText: "pinner account api-keys create <name>",
				Description: `Create a new API key with the given name.

The API key token will be displayed once. Save it securely — it cannot
be retrieved later.

This key can be used with:
  pinner auth --auth-token <token>
  PINNER_AUTH_TOKEN=<token> pinner <command>`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					output := setupOutput(cmd)
					cfgMgr, err := defaultConfigManagerFactory()
					if err != nil {
						return err
					}
					authToken := GetAuthToken(cmd, cfgMgr)
					return accountAPIKeysCreate(ctx, newCLICommandWrapper(cmd), output, cfgMgr, authToken, defaultAuthServiceFactory, defaultAPIKeyServiceFactory)
				},
			},
			{
				Name:      "delete",
				Usage:     "Delete an API key",
				UsageText: "pinner account api-keys delete <uuid-or-name>",
				Description: `Delete an API key by its UUID or name.

If the key being deleted is the one currently used for authentication,
the command will be blocked unless --force is used. After deleting your
current key, you must re-authenticate with 'pinner auth'.`,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    FlagForce,
						Aliases: []string{"f"},
						Usage:   "Force deletion of the API key currently used for authentication",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					output := setupOutput(cmd)
					cfgMgr, err := defaultConfigManagerFactory()
					if err != nil {
						return err
					}
					authToken := GetAuthToken(cmd, cfgMgr)
					return accountAPIKeysDelete(ctx, newCLICommandWrapper(cmd), output, cfgMgr, authToken, defaultAuthServiceFactory, defaultAPIKeyServiceFactory)
				},
			},
		},
	}
}

func accountAPIKeysList(ctx context.Context, cmd flagGetter, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)
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
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)
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
	output.Print("Save this token securely — it cannot be retrieved later.")
	output.Print("Use it with: pinner auth --auth-token <token>")

	return nil
}

func accountAPIKeysDelete(ctx context.Context, cmd argsFlagGetterWithBool, output Output, cfgMgr config.Manager, authToken string, authServiceFactory AuthServiceFactory, svcFactory APIKeyServiceFactory) error {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)
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
