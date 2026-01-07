package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func newConfirmEmailCommand() *cli.Command {
	return &cli.Command{
		Name:  "confirm-email",
		Usage: "Confirm your email address",
		Description: `Confirm your email address using the verification token sent to your email.

After registering with 'pinner register', you will receive an email with a
verification token. Use this command to confirm your email address.

Examples:
  pinner confirm-email --email user@example.com --token abc123def456
  pinner confirm-email -e user@example.com -t abc123def456

After confirmation, authenticate with:
  pinner auth --email user@example.com`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagEmail,
				Aliases:  []string{"e"},
				Usage:    "Email address",
				Required: true,
			},
			&cli.StringFlag{
				Name:     FlagToken,
				Aliases:  []string{"t"},
				Usage:    "Verification token from email",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(cmd.Bool(FlagJSON), cmd.Bool(FlagVerbose), cmd.Bool(FlagQuiet), cmd.Bool(FlagUnmask))
			return confirmEmail(ctx, cmd, output, defaultConfigManagerFactory)
		},
	}
}

func confirmEmail(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory) error {
	email := cmd.String(FlagEmail)
	token := cmd.String(FlagToken)

	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()

	accountClient := portalsdk.NewClient(portalsdk.WithEndpoint(apiEndpoint))

	err = accountClient.VerifyEmail(ctx, email, token)
	if err != nil {
		return fmt.Errorf("email verification failed: %w", err)
	}

	output.Print("Email verified successfully!")
	output.Printf("You can now authenticate with: pinner auth --email %s", email)
	return nil
}
