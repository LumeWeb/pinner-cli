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
		Description: `Run the interactive setup wizard to configure your Pinner.xyz CLI environment (authentication + configuration). Requires an interactive terminal; in non-interactive/agent (MCP) contexts this command fails, so configure directly with 'auth' and 'config set' instead. If you've already run setup, you can skip steps or reconfigure.

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
	authService := authServiceFactory(cfgMgr, apiEndpoint)

	if ui == nil {
		ui = NewPTermSetupUI(output)
	}

	w := NewSetupWizard(cfgMgr, authService, ui, SetupOptions{
		SkipAuth:   skipAuth,
		SkipConfig: skipConfig,
		Reset:      reset,
	})

	// Chain setup -> mcp install via the composition seam. The install flow is
	// offered as an opt-in step and, when accepted, runs as a nested
	// sub-wizard (RunMcpInstallWizard) over the same terminal channel.
	//
	// The embedded install gets a DEDICATED interactive flag surface, not the
	// setup command itself: pinner setup registers no mcp install flags, and
	// passing setup's *cli.Command into RunMcpInstallWizard would make its
	// real-command branch (mcp_install.go) splice the HTTP/service tunnel
	// collector from a flag set setup never exposes. Through a plain getter
	// (which is not a *cli.Command) the embedded flow stays an interactive
	// stdio install — consistent with setup being interactive-only — and never
	// misreads setup-unregistered flags. The install prompts for agent
	// detection / scope / transport and defaults to stdio.
	if _, isReal := cmd.(mcpInstallFlagGetter); isReal {
		interactive := &setupMcpInstallFlags{}
		w = w.WithMcpInstaller(func(ctx context.Context, _ *SetupWizard) error {
			return RunMcpInstallWizard(ctx, interactive, nil, nil)
		})
	}

	_, err = w.Run(ctx)
	return err
}

// setupMcpInstallFlags is the flag surface the embedded mcp install reads when
// it is chained from pinner setup. It is deliberately NOT a *cli.Command: it
// yields interactive defaults (empty agents/scope/transport, non-interactive
// false) so RunMcpInstallWizard prompts for agent detection / scope /
// transport and defaults to stdio, and its real-command branch (which splices
// the HTTP/service tunnel collector from registered flags) never fires — pinner
// setup registers no mcp install flags, so that branch would otherwise read a
// flag surface the host command does not expose.
type setupMcpInstallFlags struct{}

var _ mcpInstallFlagGetter = (*setupMcpInstallFlags)(nil)

func (s *setupMcpInstallFlags) String(string) string        { return "" }
func (s *setupMcpInstallFlags) Bool(string) bool            { return false }
func (s *setupMcpInstallFlags) Int(string) int              { return 0 }
func (s *setupMcpInstallFlags) IsSet(string) bool           { return false }
func (s *setupMcpInstallFlags) StringSlice(string) []string { return nil }
