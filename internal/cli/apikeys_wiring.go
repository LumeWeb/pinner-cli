package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	portalsdk "go.lumeweb.com/portal-sdk"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/apikeys"
	"go.lumeweb.com/pinner/core/auth"
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
			// The per-invocation --auth-token flag (put in the input map by the
			// shared pipeline's HonorAuthTokenOverride) takes precedence over the
			// config token.
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

// newAPIKeysCommand builds the catalog-driven "api-keys" parent command. It
// declares the api-keys operations' command shape in internal/clicatalog/shapes_apikeys.go
// and materializes the tree through mount-owned leaf and parent builders. The
// positional <name>/<id> mapping, the --force gate for delete, the
// per-invocation --auth-token override and the renderer all stay mount-owned
// here via apikeysCatalogConfig. newAccountAPIKeysCommand (in
// account_api_keys.go) delegates to this.
func newAPIKeysCommand() *cli.Command {
	root := clicatalog.APIKeysDomainRoot
	ops := catalogops.APIKeysOperations(apiKeysCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.APIKeysShapes,
		root,
		apikeysCatalogConfig(),
		buildAPIKeysLeaf,
		buildCLIParent,
	)
	if err != nil {
		panic(fmt.Sprintf("catalog compile api-keys: %v", err))
	}

	return &cli.Command{
		Name:        root.Name,
		Aliases:     root.Aliases,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands:    cmds,
	}
}

// buildAPIKeysLeaf is the mount-owned leaf builder materializing one api-keys
// leaf into an urfave *cli.Command. Shape (name/category/aliases/flags/usage)
// comes from the clicatalog model via NewCLILeaf; behavior (the catalog action
// adapter + relaxFlagRequired) stays mount-owned here. api-keys needs no
// mount-added flags beyond the compiled leaf flags.
func buildAPIKeysLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// apikeysCatalogConfig returns the CatalogAdapterConfig that expresses the
// api-keys domain's exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline. API keys needs four hooks: the result
// renderer, the per-invocation --auth-token override, the positional
// <name>/<id> mapping onto both the "name" and "id" args when empty, and the
// delete passthrough gate. All remaining fields stay nil so the shared
// pipeline's safe defaults apply.
func apikeysCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderAPIKeysResult,
		HonorAuthTokenOverride: true,

		// Map the positional <name>/<id> into the "name" and "id" args when
		// empty. Both are checked so a single positional serves api_keys_create
		// (name arg) and api_keys_delete (id arg) alike, without clobbering a
		// value already supplied via a flag.
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			if ic.C.Args().Len() > 0 {
				if hasArg(ic.Op, "name") && opmesh.StrArg(ic.Input, "name", "") == "" {
					ic.Input["name"] = ic.C.Args().First()
				}
				if hasArg(ic.Op, "id") && opmesh.StrArg(ic.Input, "id", "") == "" {
					ic.Input["id"] = ic.C.Args().First()
				}
			}
			return nil
		},

		// api keys delete passes --force through to the handler and lets the
		// core service decide (no CLI refusal; the hidden --confirm alias is
		// not honored here). GatePassthroughForce writes input["confirm"] from
		// --force only and never handles, exactly matching the original
		// adapter's SafetyDestructive branch.
		DestructiveGate: GatePassthroughForce(),
	}
}

// renderAPIKeysResult renders an api-keys handler's typed DATA through the CLI
// Output formatter.
func renderAPIKeysResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
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
