package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// account_wiring.go adapts the account catalog operations
// (internal/catalogops/account_ops.go) to urfave/cli/v3 commands mounted under
// the `account` parent. It injects the config manager + auth service (honoring
// the --auth-token override), maps the positional <email> / password flags onto
// the operation input, renders typed results, and powers the `--open`
// convenience that spawns the user's default browser at the subscription page.
//
// Because these are catalog operations, the same definitions compile to the MCP
// tool surface (via buildCatalogOpsDeps), so an account control added here is
// reachable from `pinner account ...` AND as an MCP tool.

// accountCatalogDeps builds catalogops.AccountDeps for the CLI frontend.
func accountCatalogDeps() catalogops.AccountDeps {
	return catalogops.AccountDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		// Build an auth service for the live config's account endpoint,
		// honoring the per-invocation --auth-token override ("" = use config).
		AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
			endpoint := cfgMgr.Config().GetAccountEndpointSecure()
			if token != "" {
				return defaultAuthServiceFactoryWithToken(cfgMgr, endpoint, token)
			}
			return defaultAuthServiceFactory(cfgMgr, endpoint)
		},
		// Web-app subscription page URL: https://account.<portal>/account/subscription.
		PortalURL: func(cfgMgr config.Manager) string {
			return strings.TrimSuffix(cfgMgr.Config().GetAccountEndpointSecure(), "/") + "/account/subscription"
		},
	}
}

var accountCatalogDepsVar = catalogops.AccountDeps(accountCatalogDeps())

// accountWiringParent builds the catalog-driven subcommands for the `account`
// parent (info, email, password, subscription, portal). newAccountCommand
// merges these with the existing hand-written otp/api-keys subcommands.
func accountWiringParent() []*cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.AccountOperations(accountCatalogDepsVar) {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile account: %v", err))
	}

	out := make([]*cli.Command, 0, len(compiled))
	for _, c := range compiled {
		canonical := c.Name // e.g. "account_info"
		leaf := canonical[len("account_"):]
		c.Name = leaf
		c.Category = "Management"
		relaxFlagRequired(c)

		var op catalog.Operation
		for _, cand := range catalogops.AccountOperations(accountCatalogDepsVar) {
			if cand.Name() == canonical {
				op = cand
				break
			}
		}
		if op != nil {
			c.Action = accountActionAdapter(op)
			// The `--open` convenience is CLI-only (spawns the default browser
			// at the returned web URL); it is not part of the data contract and
			// never appears on the MCP surface. Add it to read commands that
			// surface a web URL.
			if op.Name() == "account_subscription" || op.Name() == "account_portal" {
				c.Flags = append(c.Flags, &cli.BoolFlag{
					Name:  "open",
					Usage: "Open the subscription page in your default browser",
				})
			}
		}
		out = append(out, c)
	}
	return out
}

// accountActionAdapter wraps a catalog account command's Action: maps the
// positional <email> / password flags into the operation input, threads the
// --auth-token override, and renders the typed result. It also handles the
// `--open` convenience on read commands that return a web URL.
func accountActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// Per-invocation --auth-token override.
		if tok := c.String(FlagAuthToken); tok != "" {
			input[catalogops.AuthTokenInputKey] = tok
		}

		// Map the positional <email> into the "email" arg when empty.
		if c.Args().Len() > 0 {
			if hasArg(op, "email") && catalog.StrArg(input, "email", "") == "" {
				input["email"] = c.Args().First()
			}
		}

		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}

		// --open convenience: print, then spawn the default browser at the URL.
		// Human-readable browser messages must never pollute stdout in
		// --json / --agent modes (they would corrupt the structured result);
		// the browser still opens, only the chatter is suppressed.
		if shouldOpen(c) {
			output := setupOutput(c)
			if url := accountResultURL(result); url != "" {
				if perr := openURL(url); perr != nil {
					// Non-fatal: the URL is printed regardless.
					if !output.IsJSON() {
						output.Printfln("Could not auto-open the browser: %v", perr)
					}
				} else if !output.IsJSON() {
					output.Printfln("Opened %s in your browser.", url)
				}
			}
		}

		return renderAccountResult(ctx, c, op, result)
	}
}

// shouldOpen reports whether the command's --open flag was set.
func shouldOpen(c *cli.Command) bool {
	for _, f := range c.Flags {
		if bf, ok := f.(*cli.BoolFlag); ok && bf.Name == "open" {
			return c.Bool("open")
		}
	}
	return false
}

// accountResultURL extracts the web URL from a typed account result, if any.
func accountResultURL(result any) string {
	switch r := result.(type) {
	case *catalogops.AccountSubscriptionResult:
		return r.WebURL
	case *catalogops.AccountPortalResult:
		return r.URL
	}
	return ""
}

// renderAccountResult renders an account handler's typed DATA through the CLI
// Output formatter.
func renderAccountResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *catalogops.AccountInfoResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"Email", r.Email},
			{"Name", strings.TrimSpace(r.FirstName + " " + r.LastName)},
			{"User ID", fmt.Sprintf("%d", r.UserID)},
			{"Email Verified", fmt.Sprintf("%v", r.Verified)},
			{"2FA Enabled", fmt.Sprintf("%v", r.OTP)},
		}})
		return nil

	case *catalogops.AccountUpdateEmailResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r.Message != "" {
			output.Printfln("%s", r.Message)
			return nil
		}
		output.Printfln("Email updated.")
		return nil

	case *catalogops.AccountUpdatePasswordResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r.Message != "" {
			output.Printfln("%s", r.Message)
			return nil
		}
		output.Printfln("Password updated.")
		return nil

	case *catalogops.AccountSubscriptionResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r.IsSubscribed {
			output.Printfln("Subscribed.")
		} else {
			output.Printfln("Not subscribed.")
		}
		if r.WillCancelAt != nil && *r.WillCancelAt != "" {
			output.Printfln("Cancellation scheduled: %s", *r.WillCancelAt)
		}
		if r.PausedAt != nil && *r.PausedAt != "" {
			output.Printfln("Billing paused: %s", *r.PausedAt)
		}
		if r.GatewayType != nil && *r.GatewayType != "" {
			output.Printfln("Gateway: %s", *r.GatewayType)
		}
		if r.WebURL != "" {
			output.Printfln("Manage subscription: %s", r.WebURL)
		}
		return nil

	case *catalogops.AccountPortalResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r.URL != "" {
			output.Printfln("%s", r.URL)
		}
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
