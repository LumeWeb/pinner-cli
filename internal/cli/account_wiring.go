package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/flag"
	"go.lumeweb.com/pinner-cli/internal/urlopen"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/auth"
	"go.lumeweb.com/pinner/core/config"
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

// accountOTPDisableWired returns the `otp disable` subcommand. It keeps the
// hand-written command shape (the --password sensitive flag with the
// Stdin/env source chain) but routes its Action through the catalog's
// account_otp_disable operation via accountCatalogConfig / catalogActionAdapter,
// so the disable flow
// is catalog-driven (reaching core auth.DisableOTP and rendering
// AccountOTPDisableResult) just like the account operations, while the op is
// NOT emitted as a flat top-level `otp-disable` under the account parent.
func accountOTPDisableWired() *cli.Command {
	var op opmesh.Operation
	for _, cand := range catalogops.AccountOperations(accountCatalogDepsVar) {
		if cand.Name() == "account_otp_disable" {
			op = cand
			break
		}
	}
	cmd := &cli.Command{
		Name:  "disable",
		Usage: "Disable two-factor authentication",
		Description: `Disable 2FA for your account. Provide your current password via --password (or PINNER_PASSWORD / stdin); the password is not prompted interactively.

WARNING: This reduces your account security. Consider re-enabling 2FA.`,
		Flags: []cli.Flag{
			flag.SensitiveStringFlag(&cli.StringFlag{
				Name:    FlagPassword,
				Aliases: []string{"p"},
				Usage:   "Password for verification (WARNING: insecure, prefer stdin or prompt)",
				Sources: cli.NewValueSourceChain(
					Stdin(),
					cli.EnvVar("PINNER_PASSWORD"),
				),
			}),
		},
	}
	if op != nil {
		cmd.Action = catalogActionAdapter(op, accountCatalogConfig())
	}
	return cmd
}

// newAccountCatalogCommands compiles the account catalog operations into the
// subcommands mounted under the `account` parent (info, update-email,
// update-password, subscription, quota) via the shape model in
// internal/clicatalog/shapes_account.go. The account_otp_disable op is
// EXCLUDED here by the shape registry (ExcludeFromFlatMount): it is not a flat
// `account otp-disable` leaf — it nests under the hand-written `otp` parent as
// `account otp disable` (see accountOTPDisableWired). newAccountCommand merges
// these with the hand-written otp/api-keys subcommands.
func newAccountCatalogCommands() []*cli.Command {
	root := clicatalog.AccountDomainRoot
	ops := catalogops.AccountOperations(accountCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.AccountShapes,
		root,
		accountCatalogConfig(),
		buildAccountLeaf,
		buildCLIParent,
	)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the account group.
		panic(fmt.Sprintf("catalog compile account: %v", err))
	}
	return cmds
}

// buildAccountLeaf is the mount-owned leaf builder materializing one account
// leaf into an urfave *cli.Command. Shape (name/category/aliases/flags/usage)
// comes from the clicatalog model via NewCLILeaf; behavior (the catalog action
// adapter + relaxFlagRequired) stays mount-owned here.
//
// The one account-specific addition beyond the generic leaf builder is the
// CLI-only `--open` convenience on the read commands that surface a web URL
// (subscription, quota): it spawns the default browser at the returned deep
// link. It is not part of the data contract (never on the MCP surface), so it
// lives here in the wiring layer with the hand-written mount.
func buildAccountLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	switch loc.Op.Name() {
	case "account_subscription":
		base.Flags = append(base.Flags, &cli.BoolFlag{
			Name:  "open",
			Usage: "Open the subscription page in your default browser",
		})
	case "account_quota":
		base.Flags = append(base.Flags, &cli.BoolFlag{
			Name:  "open",
			Usage: "Open the account/usage page in your default browser",
		})
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// accountCatalogConfig returns the CatalogAdapterConfig that expresses
// account's exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline. Account needs four hooks: the result
// renderer, the per-invocation --auth-token override, the positional <email>
// mapping onto the "email" arg, and the post-execute `--open` convenience that
// spawns the default browser at a returned web URL. All remaining fields stay
// nil so the shared pipeline's safe defaults apply (account ops are
// read/mutate, never destructive, so no destructive gate runs).
func accountCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderAccountResult,
		HonorAuthTokenOverride: true,

		// Map the positional <email> into the "email" arg when empty (the
		// account email op accepts the new email positionally).
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			if ic.C.Args().Len() > 0 {
				if hasArg(ic.Op, "email") && opmesh.StrArg(ic.Input, "email", "") == "" {
					ic.Input["email"] = ic.C.Args().First()
				}
			}
			return nil
		},

		// --open convenience: print, then spawn the default browser at the URL.
		// Human-readable browser messages must never pollute stdout in
		// --json / --agent modes (they would corrupt the structured result);
		// the browser still opens, only the chatter is suppressed.
		PostExecute: func(ic *CatalogInvokeContext, result any) error {
			if shouldOpen(ic.C) {
				if url := accountResultURL(result); url != "" {
					if perr := urlopen.Open(url); perr != nil {
						// Non-fatal: the URL is printed regardless.
						if !ic.Output.IsJSON() {
							ic.Output.Printfln("Could not auto-open the browser: %v", perr)
						}
					} else if !ic.Output.IsJSON() {
						ic.Output.Printfln("Opened %s in your browser.", url)
					}
				}
			}
			return nil
		},
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
	case *catalogops.AccountQuotaResult:
		return r.WebURL
	}
	return ""
}

// renderAccountResult renders an account handler's typed DATA through the CLI
// Output formatter.
func renderAccountResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
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

	case *catalogops.AccountOTPDisableResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if r.Message != "" {
			output.Printfln("%s", r.Message)
			return nil
		}
		output.Printfln("Two-factor authentication disabled.")
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

	case *catalogops.AccountQuotaResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Quota usage:")
		printQuotaType := func(label string, q catalogops.AccountQuotaType) {
			limitStr := "unlimited"
			if q.Limit != nil {
				limitStr = fmt.Sprintf("%d", *q.Limit)
			}
			remStr := "n/a"
			if q.Remaining != nil {
				remStr = fmt.Sprintf("%d", *q.Remaining)
			}
			output.Printfln("  %-8s used=%d limit=%s remaining=%s (%d%%)", label, q.Used, limitStr, remStr, q.Percentage)
		}
		printQuotaType("upload", r.Upload)
		printQuotaType("download", r.Download)
		printQuotaType("storage", r.Storage)
		if r.HasQuota {
			output.Printfln("Covered by granted quota; no subscription required.")
		} else {
			output.Printfln("No usable quota remaining; a subscription or additional granted usage is required.")
		}
		if r.WebURL != "" {
			output.Printfln("Manage usage / subscribe: %s", r.WebURL)
		}
		if r.Message != "" {
			output.Printfln("%s", r.Message)
		}
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}
