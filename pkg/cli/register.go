package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newRegisterCommand() *cli.Command {
	return &cli.Command{
		Name:     "register",
		Category: "Setup",
		Usage:    "Create a new account",
		Description: `Register a new user account on Pinner.xyz.

After registration, you will need to confirm your email address using the
'confirm-email' command before you can authenticate.

Examples:
  # Interactive mode (prompts for all required fields)
  pinner register

  # Non-interactive with positional email
  pinner register user@example.com --first-name John --last-name Doe --password secret

  # Non-interactive with flags
  pinner register --email user@example.com --first-name John --last-name Doe

  # Mix: provide email, prompt for other fields
  pinner register user@example.com`,
		ArgsUsage: "[email]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagEmail,
				Aliases: []string{"e"},
				Usage:   "Email address",
			},
			&cli.StringFlag{
				Name:    FlagFirstName,
				Aliases: []string{"f"},
				Usage:   "First name",
			},
			&cli.StringFlag{
				Name:    FlagLastName,
				Aliases: []string{"l"},
				Usage:   "Last name",
			},
			&cli.StringFlag{
				Name:    FlagPassword,
				Aliases: []string{"p"},
				Usage:   "Password (if not provided, you will be prompted)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return register(ctx, newCLICommandWrapper(cmd), output, defaultConfigManagerFactory, defaultAuthServiceFactory)
		},
	}
}

func register(ctx context.Context, cmd argsFlagGetterWithBool, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	email := cmd.String(FlagEmail)
	// Fall back to positional arg if --email flag is not set: pinner register user@example.com
	if email == "" && cmd.Args().Len() > 0 {
		email = cmd.Args().First()
	}
	firstName := cmd.String(FlagFirstName)
	lastName := cmd.String(FlagLastName)
	password := cmd.String(FlagPassword)

	prompter := &promptuiPrompter{}
	var err error

	if email == "" {
		email, err = prompter.PromptEmail()
		if err != nil {
			return fmt.Errorf("failed to read email: %w", err)
		}
	}

	if firstName == "" {
		firstName, err = prompter.PromptString("First name")
		if err != nil {
			return fmt.Errorf("failed to read first name: %w", err)
		}
	}

	if lastName == "" {
		lastName, err = prompter.PromptString("Last name")
		if err != nil {
			return fmt.Errorf("failed to read last name: %w", err)
		}
	}

	if password == "" {
		password, err = prompter.PromptPassword()
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}

		confirmPassword, err := prompter.PromptPassword()
		if err != nil {
			return fmt.Errorf("failed to read password confirmation: %w", err)
		}

		if password != confirmPassword {
			return fmt.Errorf("passwords do not match")
		}
	}

	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	return authService.Register(ctx, email, firstName, lastName, password)
}
