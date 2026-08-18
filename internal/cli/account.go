package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newAccountCommand() *cli.Command {
	// Catalog-driven account subcommands (info, email, password, subscription,
	// portal) compile to both the CLI and MCP surfaces; merge them under the
	// `account` parent alongside the hand-written otp/api-keys subcommands.
	catalogCmds := accountWiringParent()
	return &cli.Command{
		Name:     "account",
		Category: "Setup",
		Usage:    "Manage account settings",
		Description: `Manage your Pinner.xyz account profile, email, password, subscription, 2FA configuration, and API keys.

		Examples:
		pinner account info
		pinner account update-email you@example.com --password currentpass
		pinner account update-password
		pinner account subscription
		pinner account subscription --open
		pinner account otp enable
		pinner account otp disable --password mypassword`,
		Commands: append(
			[]*cli.Command{
				newAccountOTPCommand(),
				// api-keys is catalog-driven but kept as its own parent.
				newAccountAPIKeysCommand(),
			},
			catalogCmds...,
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
