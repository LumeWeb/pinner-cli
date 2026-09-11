package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
)

func newAccountCommand() *cli.Command {
	// The account parent is catalog-driven: the non-interactive subcommands
	// (info, update-email, update-password, subscription, quota) are compiled
	// from the canonical operation catalog — see newAccountCatalogCommands in
	// account_wiring.go and the shape model in internal/clicatalog/shapes_account.go.
	// The `otp` (its enable is hand-written interactive; disable is
	// catalog-wired under the parent) and `api-keys` parents are hand-written
	// and merged under the same root.
	root := clicatalog.AccountDomainRoot
	return &cli.Command{
		Name:        root.Name,
		Category:    root.Category,
		Usage:       root.Usage,
		Description: root.Desc,
		Commands: append(
			[]*cli.Command{
				newAccountOTPCommand(),
				// api-keys is catalog-driven but kept as its own parent.
				newAccountAPIKeysCommand(),
			},
			newAccountCatalogCommands()...,
		),
	}
}

func newAccountOTPCommand() *cli.Command {
	return &cli.Command{
		Name:  "otp",
		Usage: "Manage two-factor authentication",
		Description: `Enable or disable two-factor authentication (2FA) for your account.

When enabling 2FA, you will receive a secret key that must be added to your
authenticator app (e.g., Google Authenticator, Authy). You will then need to
verify the setup with a code from your app.`,
		Commands: []*cli.Command{
			{
				Name:  "enable",
				Usage: "Enable two-factor authentication",
				Description: `Enable 2FA for your account. This will:
  1. Generate a new OTP secret
  2. Display a QR code/secret key for your authenticator app
  3. Prompt you to verify the setup with a code from your app

After successful verification, 2FA will be required for all future logins.`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    FlagOTP,
						Aliases: []string{"o"},
						Usage:   "OTP code to verify setup (for non-interactive mode)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					output := setupOutput(cmd)
					return accountOTPEnable(ctx, newCLICommandWrapper(cmd), output, defaultConfigManagerFactory, defaultAuthServiceFactory)
				},
			},
			// disable is catalog-driven: the command shape (name, usage,
			// --password sensitive flag) is preserved here, but its Action is
			// wired through the account_otp_disable catalog operation so the
			// flow is defined once and compiled to both the CLI and MCP
			// surfaces. See accountOTPDisableWired in account_wiring.go.
			accountOTPDisableWired(),
		},
	}
}

func accountOTPEnable(ctx context.Context, cmd flagGetter, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, apiEndpoint)

	otpCode := cmd.String(FlagOTP)

	// Generate the OTP secret so the user can add it to their authenticator app.
	secretRes, err := authService.GenerateOTPSecret(ctx)
	if err != nil {
		return err
	}
	renderOTPSecret(output, secretRes.Secret)

	// If no OTP code was provided, prompt for it interactively.
	if otpCode == "" {
		prompter := &promptuiPrompter{}
		otpCode, err = prompter.PromptOTP()
		if err != nil {
			return fmt.Errorf("failed to read OTP code: %w", err)
		}
	}

	if err := authService.VerifyOTP(ctx, otpCode); err != nil {
		return err
	}
	renderOTPEnabled(output)
	return nil
}
