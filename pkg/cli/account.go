package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newAccountCommand() *cli.Command {
	return &cli.Command{
		Name:  "account",
		Usage: "Manage account settings",
		Description: `Manage your Pinner.xyz account settings including 2FA configuration.

Examples:
  pinner account otp enable
  pinner account otp enable --otp 123456
  pinner account otp disable
  pinner account otp disable --password mypassword`,
		Commands: []*cli.Command{
			newAccountOTPCommand(),
		},
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
					return accountOTPEnable(ctx, cmd, output, defaultConfigManagerFactory, defaultAuthServiceFactory)
				},
			},
			{
				Name:  "disable",
				Usage: "Disable two-factor authentication",
				Description: `Disable 2FA for your account. This will:
  1. Prompt for your password for verification
  2. Remove 2FA requirement from your account

WARNING: This reduces your account security. Consider re-enabling 2FA.`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    FlagPassword,
						Aliases: []string{"p"},
						Usage:   "Password for verification (WARNING: insecure, prefer stdin or prompt)",
						Sources: cli.NewValueSourceChain(
							Stdin(),
							cli.EnvVar("PINNER_PASSWORD"),
						),
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					output := setupOutput(cmd)
					return accountOTPDisable(ctx, cmd, output, defaultConfigManagerFactory, defaultAuthServiceFactory)
				},
			},
		},
	}
}

func accountOTPEnable(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	otpCode := cmd.String(FlagOTP)

	return authService.EnableOTP(ctx, otpCode)
}

func accountOTPDisable(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	password := cmd.String("password")

	return authService.DisableOTP(ctx, password)
}
