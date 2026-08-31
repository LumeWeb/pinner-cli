//go:build !no_tunnel

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
	// The embedded install runs on a dedicated shadow *cli.Command carrying the
	// full `pinner mcp install` flag surface (base wizard flags + the shared
	// tunnel/environment flags), NOT the setup command itself: pinner setup
	// registers no mcp install flags, so passing setup's own *cli.Command into
	// RunMcpInstallWizard would make its real-command branch read a flag set
	// setup never exposes. Giving the shadow command the real install flag
	// surface means the HTTP/service composite collector (which requires a real
	// *cli.Command) wires correctly, so the operator can pick stdio OR http
	// exactly as with `pinner mcp install` — an http choice prompts for the
	// tunnel/service through the interactive service wizard instead of
	// silently failing. Everything is interactive (setup requires a TTY), so
	// all flags stay unset and the wizard prompts for agent / scope /
	// transport just like the standalone command.
	if _, isReal := cmd.(mcpInstallFlagGetter); isReal {
		embedded := embeddedMcpInstallCommand()
		w = w.WithMcpInstaller(func(ctx context.Context, _ *SetupWizard) error {
			return RunMcpInstallWizard(ctx, embedded, nil, nil)
		})
	}

	_, err = w.Run(ctx)
	return err
}

// embeddedMcpInstallCommand is the shadow *cli.Command the setup-chained MCP
// install runs through. It carries the same flag surface as `pinner mcp
// install` (base wizard flags + the shared tunnel/environment flags) but with
// every flag at its default and no CLI wiring — RunMcpInstallWizard only
// reads it as a flag surface. Being a real *cli.Command is what lets the
// HTTP/service composite collector wire: it type-asserts cmd.(*cli.Command)
// and needs the tunnel/env flags registered to resolve MCP_AUTH_TOKEN,
// MCP_TUNNEL_PROVIDER, MCP_PUBLIC_URL, etc. identically to the standalone
// command. Because setup is interactive and all flags are unset, the wizard
// prompts for agent / scope / transport just like `pinner mcp install`, and an
// http choice runs the interactive service wizard to configure the tunnel —
// so the operator gets the full stdio-or-http choice, not a stdio-only or
// silently-broken http path.
func embeddedMcpInstallCommand() *cli.Command {
	return &cli.Command{Flags: installFlags()}
}
