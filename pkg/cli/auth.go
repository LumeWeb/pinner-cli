package cli

import (
	"context"
	"fmt"
	"net/mail"

	"github.com/manifoldco/promptui"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// cleanupTerminal restores terminal state after an interrupt.
// This is necessary because Ctrl+C during promptui prompts can leave
// the cursor hidden or terminal formatting applied.
func cleanupTerminal() {
	// Show cursor if it was hidden by promptui
	fmt.Print(promptui.ResetCode)
	fmt.Print("\033[?25h") // showCursor
	fmt.Println()          // Ensure we're on a new line

	// Ensure terminal is in a clean state
	fmt.Println()
}

// handleInterrupt checks if an error is an interrupt and cleans up the terminal.
// Returns the error if it's not an interrupt, or a cancellation error if it is.
func handleInterrupt(err error) error {
	if err == promptui.ErrInterrupt {
		cleanupTerminal()
		return fmt.Errorf("cancelled")
	}
	return err
}

// runPrompt executes a prompt and handles interrupts, returning the result or error.
func runPrompt(fn func() (string, error)) (string, error) {
	result, err := fn()
	if err != nil {
		return "", handleInterrupt(err)
	}
	return result, nil
}

// commandGetter defines the interface for getting command flags and arguments.
type commandGetter interface {
	String(name string) string
	Bool(name string) bool
}

// AuthPrompter defines the interface for interactive user input.
type AuthPrompter interface {
	// PromptEmail prompts for and validates an email address.
	PromptEmail() (string, error)

	// PromptPassword prompts for and validates a password (with masking).
	PromptPassword() (string, error)

	// Password prompts for a password with masking (no validation).
	Password(label string) (string, error)

	// PromptString prompts for a string value with a label.
	PromptString(label string) (string, error)

	// PromptOTP prompts for a 6-digit OTP code.
	PromptOTP() (string, error)
}

// promptuiPrompter implements AuthPrompter using promptui.
type promptuiPrompter struct{}

// PromptEmail prompts for and validates an email address.
func (p *promptuiPrompter) PromptEmail() (string, error) {
	return runPrompt(func() (string, error) {
		emailPrompt := promptui.Prompt{
			Label: "Email",
			Validate: func(input string) error {
				if input == "" {
					return fmt.Errorf("email is required")
				}
				_, err := mail.ParseAddress(input)
				if err != nil {
					return fmt.Errorf("invalid email format: %w", err)
				}
				return nil
			},
		}
		return emailPrompt.Run()
	})
}

// PromptPassword prompts for and validates a password (with masking).
func (p *promptuiPrompter) PromptPassword() (string, error) {
	return runPrompt(func() (string, error) {
		passwordPrompt := promptui.Prompt{
			Label: "Password",
			Mask:  '*',
			Validate: func(input string) error {
				if input == "" {
					return fmt.Errorf("password is required")
				}
				return nil
			},
		}
		return passwordPrompt.Run()
	})
}

// Password prompts for a password with masking (no validation).
func (p *promptuiPrompter) Password(label string) (string, error) {
	return runPrompt(func() (string, error) {
		passwordPrompt := promptui.Prompt{
			Label: label,
			Mask:  '*',
		}
		return passwordPrompt.Run()
	})
}

// PromptString prompts for a string value with a label.
func (p *promptuiPrompter) PromptString(label string) (string, error) {
	return runPrompt(func() (string, error) {
		stringPrompt := promptui.Prompt{
			Label: label,
			Validate: func(input string) error {
				if input == "" {
					return fmt.Errorf("%s is required", label)
				}
				return nil
			},
		}
		return stringPrompt.Run()
	})
}

// PromptOTP prompts for a 6-digit OTP code.
func (p *promptuiPrompter) PromptOTP() (string, error) {
	return runPrompt(func() (string, error) {
		otpPrompt := promptui.Prompt{
			Label: "OTP Code",
			Mask:  '*',
			Validate: func(input string) error {
				if input == "" {
					return fmt.Errorf("OTP code is required")
				}
				if len(input) != 6 {
					return fmt.Errorf("OTP code must be 6 digits")
				}
				for _, c := range input {
					if c < '0' || c > '9' {
						return fmt.Errorf("OTP code must contain only digits")
					}
				}
				return nil
			},
		}
		return otpPrompt.Run()
	})
}

func newAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authenticate with Pinner.xyz",
		Description: `Authenticate with the Pinner.xyz service.

Ways to authenticate:
  1. Provide JWT token directly: pinner auth <jwt-token>
  2. Interactive login: pinner auth (prompts for all inputs)
  3. Semi-interactive: pinner auth --email user@example.com (prompts for password and OTP if needed)
  4. Non-interactive: PINNER_EMAIL=x PINNER_PASSWORD=y pinner auth
  5. Non-interactive with 2FA: PINNER_EMAIL=x PINNER_PASSWORD=y PINNER_OTP=123456 pinner auth
  6. Secure non-interactive: echo "pass" | pinner auth --email user@example.com

Examples:
  pinner auth eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
  pinner auth --email user@example.com
  pinner auth --email user@example.com --password mypass --otp-code 123456
  PINNER_EMAIL=user@example.com PINNER_PASSWORD=mypass pinner auth
  echo "mypass" | pinner auth --email user@example.com --key-name "my-laptop"`,
		ArgsUsage: "[jwt-token]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagEmail,
				Aliases: []string{"e"},
				Usage:   "Email address for login",
				Sources: cli.EnvVars("PINNER_EMAIL"),
			},
			&cli.StringFlag{
				Name:    FlagPassword,
				Aliases: []string{"p"},
				Usage:   "Password for login (WARNING: insecure, prefer stdin or env var)",
				Sources: cli.NewValueSourceChain(
					Stdin(),
					cli.EnvVar("PINNER_PASSWORD"),
				),
			},
			&cli.StringFlag{
				Name:    FlagOTPCode,
				Aliases: []string{"o"},
				Usage:   "OTP code for 2FA (6 digits)",
				Sources: cli.EnvVars("PINNER_OTP"),
			},
			&cli.StringFlag{
				Name:    FlagKeyName,
				Aliases: []string{"k"},
				Usage:   "Custom name for created API key",
				Value:   "cli-generated",
			},
			&cli.BoolFlag{
				Name:  FlagNoCreateKey,
				Usage: "Skip API key creation, save JWT directly",
			},
			&cli.BoolFlag{
				Name:  FlagForce,
				Usage: "Overwrite existing auth token without confirmation",
			},
		},
		Commands: []*cli.Command{
			newAuthStatusCommand(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)

			args := cmd.Args()
			if args.Len() > 0 {
				jwtToken := args.Get(0)
				return saveAuthToken(output, jwtToken)
			}

			return authLogin(ctx, cmd, output, defaultConfigManagerFactory, defaultAuthServiceFactory)
		},
	}
}

func saveAuthToken(output Output, token string) error {
	return saveAuthTokenWithFactories(output, token, defaultConfigManagerFactory, defaultAuthServiceFactory)
}

// saveAuthTokenWithFactories is the testable implementation of saveAuthToken.
func saveAuthTokenWithFactories(output Output, token string, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	return authService.SaveToken(token)
}

// ConfigManagerFactory creates a config manager for testing.
type ConfigManagerFactory func() (config.Manager, error)

// AuthServiceFactory creates an auth service for testing.
type AuthServiceFactory func(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService

// authLogin handles authentication with interactive, semi-interactive, and non-interactive modes.
func authLogin(ctx context.Context, cmd commandGetter, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	return authLoginWithFactories(ctx, cmd, output, cfgMgrFactory, authServiceFactory, nil)
}

// authLoginWithFactories is the testable implementation of authLogin with prompter injection.
// The factories and prompter allow dependency injection for testing.
func authLoginWithFactories(ctx context.Context, cmd commandGetter, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory, prompter AuthPrompter) error {
	email := cmd.String("email")
	password := cmd.String("password")
	otpCode := cmd.String("otp-code")
	keyName := cmd.String("key-name")
	noCreateKey := cmd.Bool("no-create-key")
	force := cmd.Bool("force")

	// Initialize config manager and auth service
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	// Use provided prompter or default to promptui
	if prompter == nil {
		prompter = &promptuiPrompter{}
	}

	// Determine mode based on provided inputs
	if email != "" {
		// Semi-interactive or non-interactive mode
		if password == "" {
			// Prompt for password only
			password, err = prompter.PromptPassword()
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
		}

		// Attempt login
		loginResult, err := authService.LoginCheck(ctx, email, password)
		if err != nil {
			return fmt.Errorf("%s", FormatError(err, output.IsVerbose()))
		}

		// Check if 2FA is required
		if loginResult.OTPRequired {
			if otpCode == "" {
				// Semi-interactive: prompt for OTP only
				otpCode, err = prompter.PromptOTP()
				if err != nil {
					return fmt.Errorf("failed to read OTP code: %w", err)
				}
			}

			return authService.LoginWithOTP(ctx, loginResult.IntermediateJWT, otpCode, keyName, noCreateKey)
		}

		// No 2FA required, complete login
		return authService.CompleteLogin(ctx, loginResult.Token, keyName, noCreateKey)
	}

	// Fully interactive mode
	return interactiveLogin(ctx, authService, output, keyName, noCreateKey, force, prompter)
}

// interactiveLogin handles fully interactive authentication including 2FA flow.
func interactiveLogin(ctx context.Context, authService AuthService, output Output, keyName string, noCreateKey, force bool, prompter AuthPrompter) error {
	email, err := prompter.PromptEmail()
	if err != nil {
		return fmt.Errorf("failed to read email: %w", err)
	}

	password, err := prompter.PromptPassword()
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	// Attempt login
	loginResult, err := authService.LoginCheck(ctx, email, password)
	if err != nil {
		return fmt.Errorf("%s", FormatError(err, output.IsVerbose()))
	}

	// Check if 2FA is required
	if loginResult.OTPRequired {
		// Prompt for OTP code (intermediate JWT handled internally)
		otpCode, err := prompter.PromptOTP()
		if err != nil {
			return fmt.Errorf("failed to read OTP code: %w", err)
		}

		return authService.LoginWithOTP(ctx, loginResult.IntermediateJWT, otpCode, keyName, noCreateKey)
	}

	// No 2FA required, complete login
	return authService.CompleteLogin(ctx, loginResult.Token, keyName, noCreateKey)
}

// defaultConfigManagerFactory creates a config manager using the default config path.
func defaultConfigManagerFactory() (config.Manager, error) {
	configPath := config.DefaultConfigPath
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfgMgr.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfgMgr, nil
}

// defaultAuthServiceFactory creates an auth service with the given dependencies.
func defaultAuthServiceFactory(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService {
	return NewAuthService(cfgMgr, output, apiEndpoint)
}

// newAuthStatusCommand creates the auth status subcommand.
func newAuthStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Check authentication status",
		Description: `Check if you are currently authenticated with Pinner.xyz.

This command verifies that your stored auth token is valid by making a
request to the Pinner.xyz API.

Examples:
  pinner auth status
  pinner auth status --json
  pinner auth status --verbose`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := NewOutputFormatter(
				cmd.Bool(FlagJSON),
				cmd.Bool(FlagVerbose),
				cmd.Bool(FlagQuiet),
				cmd.Bool(FlagUnmask),
			)
			return authStatus(ctx, cmd, output, defaultConfigManagerFactory, defaultAuthServiceFactory)
		},
	}
}

// authStatus checks if the user is authenticated.
func authStatus(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory, authServiceFactory AuthServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	return authService.Status(ctx)
}
