package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	portalsdk "go.lumeweb.com/portal-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/apikeys"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
)

// apikeys_wiring.go adapts the api-keys catalog operations
// (internal/catalogops/apikeys.go) to urfave/cli/v3 commands. It injects a
// concrete apikeys.Service and maps CLI concerns (positional <name>/<id>, the
// --force gate for delete) onto the catalog, then renders typed results.

// catalogAPIKeysDeps builds catalogops.APIKeysDeps with a live apikeys.Service
// constructed per invocation from the auth token (flag override then config).
func catalogAPIKeysDeps(factory ...ConfigManagerFactory) catalogops.APIKeysDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.APIKeysDeps{
		Service: func(input map[string]any) apikeys.Service {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			// The per-invocation --auth-token flag (put in the input map by
			// apiKeysActionAdapter) takes precedence over the config token.
			// When present, pin the authService to the override so
			// List/Create/Delete authenticate with it (not just the self-delete
			// gating helpers).
			endpoint := cfgMgr.Config().GetAPIEndpoint()
			var authService auth.AuthService
			token := cfgMgr.Config().AuthToken
			if t, ok := input[catalogops.AuthTokenInputKey].(string); ok && t != "" {
				token = t
				authService = defaultAuthServiceFactoryWithToken(cfgMgr, endpoint, t)
			} else {
				authService = defaultAuthServiceFactory(cfgMgr, endpoint)
			}
			return apikeys.New(authService, token)
		},
	}
}

var apiKeysCatalogDepsVar = catalogops.APIKeysDeps(catalogAPIKeysDeps())

// apiKeysParent builds and returns the catalog-driven "api-keys" parent.
// newAccountAPIKeysCommand (in account_api_keys.go) delegates to this.
func apiKeysParent() *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.APIKeysOperations(apiKeysCatalogDepsVar) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile api-keys: %v", err))
	}

	out := make([]*cli.Command, 0, len(compiled))
	for _, c := range compiled {
		canonical := c.Name // e.g. "api-keys.list"
		leaf := canonical[len("api_keys_"):]
		c.Name = leaf
		c.Category = "Management"
		relaxFlagRequired(c)

		var op catalog.Operation
		for _, cand := range catalogops.APIKeysOperations(apiKeysCatalogDepsVar) {
			if cand.Name() == canonical {
				op = cand
				break
			}
		}
		if op != nil {
			c.Action = apiKeysActionAdapter(op)
		}
		out = append(out, c)
	}

	return &cli.Command{
		Name:        "api-keys",
		Aliases:     []string{"apikey", "api-key"},
		Category:    "Management",
		Usage:       "Manage API keys",
		Description: "Manage API keys for your Pinner.xyz account. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
		Commands:    out,
	}
}

// apiKeysActionAdapter wraps a catalog api-keys command's Action: maps the
// positional <name>/<id> into the operation input, enforces the --force gate
// for delete, then invokes the handler and renders the result.
func apiKeysActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		input := catalog.FlagsToInput(c, op)

		// The per-invocation --auth-token override takes precedence over the
		// config token. Put it in the input so the Service closure honors it;
		// otherwise api-keys authenticate with the config token.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <name>/<id> into the "name"/"id" arg when empty.
		if c.Args().Len() > 0 {
			if hasArg(op, "name") && catalog.StrArg(input, "name", "") == "" {
				input["name"] = c.Args().First()
			}
			if hasArg(op, "id") && catalog.StrArg(input, "id", "") == "" {
				input["id"] = c.Args().First()
			}
		}

		// Destructive gate (delete). Unlike pins rm, api-keys delete does not
		// require --force in general: the core service enforces the rule that
		// deleting the currently-authenticating key needs confirmation. So we
		// pass the flag through to the handler (input["confirm"]) and let the
		// service decide. The compiler still injects --force onto the
		// destructive command; here we just map it into the operation input.
		if op.Safety() == catalog.SafetyDestructive {
			input["confirm"] = c.Bool(FlagForce)
		}

		// Apply the configured per-command timeout.
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}
		return renderAPIKeysResult(ctx, c, op, result)
	}
}

// renderAPIKeysResult renders an api-keys handler's typed DATA through the CLI
// Output formatter.
func renderAPIKeysResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

	case *portalsdk.APIKey:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{
				"uuid":  r.Uuid.String(),
				"name":  r.Name,
				"token": r.Token,
			})
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"UUID", r.Uuid.String()},
			{"Name", r.Name},
			{"Token", r.Token},
		}})
		return nil

	case *catalogops.APIKeyDeleteResult:
		if r != nil && r.Message != "" {
			output.Printfln("%s", r.Message)
			return nil
		}
		output.Printfln("API key deleted.")
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
