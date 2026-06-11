package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// newSetupCommand creates and returns the setup command.
func newSetupCommand() *cli.Command {
	return &cli.Command{
		Name:     "setup",
		Category: "Setup",
		Usage:    "Interactive first-time setup wizard",
		Description: `Run the interactive setup wizard to configure
your Pinner.xyz CLI environment. This wizard will guide you
through authentication and configuration.

If you've already run setup, you can skip steps or reconfigure.

Examples:
  pinner setup
  pinner setup --skip-auth
  pinner setup --skip-config
  pinner setup --reset`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "skip-auth",
				Usage: "Skip authentication step",
			},
			&cli.BoolFlag{
				Name:  "skip-config",
				Usage: "Skip configuration step",
			},
			&cli.BoolFlag{
				Name:  "reset",
				Usage: "Reset configuration and start fresh",
			},
			&cli.BoolFlag{
				Name:  "non-interactive",
				Usage: "Run in non-interactive mode (skip wizard)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)

			return runSetupWizard(ctx, cmd, output, defaultConfigManagerFactory, defaultAuthServiceFactory)
		},
	}
}

// runSetupCommand is the testable entry point for setup.
func runSetupWizard(
	ctx context.Context,
	cmd flagGetter,
	output Output,
	cfgMgrFactory ConfigManagerFactory,
	authServiceFactory AuthServiceFactory,
) error {
	return runSetupWizardWithFactories(ctx, cmd, output, cfgMgrFactory, authServiceFactory, nil)
}

// runSetupWizardWithFactories is the testable implementation with dependency injection.
func runSetupWizardWithFactories(
	ctx context.Context,
	cmd flagGetter,
	output Output,
	cfgMgrFactory ConfigManagerFactory,
	authServiceFactory AuthServiceFactory,
	ui SetupUI,
) error {
	skipAuth := cmd.Bool("skip-auth")
	skipConfig := cmd.Bool("skip-config")
	reset := cmd.Bool("reset")
	nonInteractive := cmd.Bool("non-interactive")

	if nonInteractive {
		return fmt.Errorf("setup wizard requires interactive mode; use 'pinner auth' and 'pinner config' commands instead")
	}

	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	authService := authServiceFactory(cfgMgr, output, apiEndpoint)

	if ui == nil {
		ui = NewPTermSetupUI(output)
	}

	w := NewSetupWizard(cfgMgr, authService, ui, SetupOptions{
		SkipAuth:   skipAuth,
		SkipConfig: skipConfig,
		Reset:      reset,
	})

	_, err = w.Run(ctx)
	return err
}
