package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	portalsdk "go.lumeweb.com/portal-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/apikeys"
)

// apikeys_wiring.go is the pkg/cli frontend adapter for the api-keys catalog
// operations (internal/catalogops/apikeys.go). The apikeys core package is
// fully implemented (no pkg/cli coupling); this wiring injects a concrete
// apikeys.Service and maps IO/CLI concerns (positional <name>/<id>, the
// --force gate for delete) onto the catalog, then renders typed results.

// catalogAPIKeysDeps builds catalogops.APIKeysDeps with a live apikeys.Service
// constructed per invocation from the auth token (flag override then config).
func catalogAPIKeysDeps() catalogops.APIKeysDeps {
	return catalogops.APIKeysDeps{
		Service: func(input map[string]any) apikeys.Service {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			authService := defaultAuthServiceFactory(cfgMgr, cfgMgr.Config().GetAPIEndpoint())
			// The per-invocation --auth-token flag (threaded through the input
			// map by apiKeysActionAdapter) takes precedence over the config
			// token, mirroring the legacy flag -> config fallback.
			token := cfgMgr.Config().AuthToken
			if t, ok := input[catalogops.AuthTokenInputKey].(string); ok && t != "" {
				token = t
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
		leaf := canonical[len("api-keys."):]
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
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// Thread the per-invocation --auth-token override into the operation
		// input so the Service closure honors it (flag -> config precedence),
		// mirroring the pins wiring. Without this, api-keys silently
		// authenticate with the config token instead of the flag.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <name>/<id> into the "name"/"id" arg when empty.
		if c.Args().Len() > 0 {
			if hasArg(op, "name") && stringVal(input["name"]) == "" {
				input["name"] = c.Args().First()
			}
			if hasArg(op, "id") && stringVal(input["id"]) == "" {
				input["id"] = c.Args().First()
			}
		}

		// Destructive gate (delete). Unlike pins rm, api-keys delete does NOT
		// require --force to run in general: the core service enforces the rule
		// that deleting the currently-authenticating key needs --force. So we
		// pass the flag through to the handler (input["force"]) and let the
		// service decide, exactly like the legacy account_api_keys.go delete —
		// a blanket 'must pass --force to delete any key' would be a regression.
		// The compiler still injects --force onto the destructive command; here
		// we simply thread it into the operation input.
		if op.Safety() == catalog.SafetyDestructive {
			input["force"] = c.Bool(FlagForce)
		}

		result, err := op.Handler().Execute(ctx, input)
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
	case *catalogops.APIKeysListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{
				"total": r.Total,
				"keys":  r.Keys,
			})
		}
		if r == nil || len(r.Keys) == 0 {
			output.Printfln("No API keys found.")
			return nil
		}
		headers := []string{"UUID", "NAME"}
		rows := make([][]string, len(r.Keys))
		for i, k := range r.Keys {
			if k == nil {
				continue
			}
			rows[i] = []string{k.Uuid.String(), k.Name}
		}
		output.PrintTable(headers, rows)
		return nil

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
