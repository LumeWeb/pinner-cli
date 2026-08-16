package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cloudflare"
)

const (
	// cloudflareTokenFlag is the flag carrying the scoped Cloudflare API token
	// (also sourced from CLOUDFLARE_API_TOKEN).
	cloudflareTokenFlag = "cloudflare-api-token"
)

// tunnelCommand returns the `pinner mcp tunnel` command group.
func tunnelCommand() *cli.Command {
	return &cli.Command{
		Name:  "tunnel",
		Usage: "Provision and install a public tunnel for the MCP server",
		Commands: []*cli.Command{
			{
				Name:  "install",
				Usage: "Provision a tunnel (deep-link API token, DNS route) and install cloudflared",
				Flags: serviceInstallFlags(),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runTunnelInstallWizard(ctx, cmd, NewServiceInstallWizardUI(), cloudflare.New)
				},
			},
			{
				Name:  "status",
				Usage: "Show the provisioned tunnel state",
				Action: func(_ context.Context, _ *cli.Command) error {
					state, err := LoadCloudflareTunnelState()
					if err != nil {
						if errors.Is(err, os.ErrNotExist) {
							return errors.New("no tunnel has been provisioned; run `pinner mcp tunnel install`")
						}
						return err
					}
					printTunnelStatus(state)
					return nil
				},
			},
		},
	}
}

// serviceInstallFlags returns the flags shared by the tunnel install and
// service install wizards (token, domain, tunnel-name, auth-token, env-file).
func serviceInstallFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: serviceTunnelFlag, Usage: "Tunnel provider: ngrok, cloudflared, or openai", Sources: cli.EnvVars("MCP_TUNNEL_PROVIDER")},
		&cli.StringFlag{Name: cloudflareTokenFlag, Usage: "Cloudflare API token (scoped to Tunnel:Edit/DNS:Edit/Zone:Read)", Sources: cli.EnvVars("CLOUDFLARE_API_TOKEN")},
		&cli.StringFlag{Name: serviceApiKeyFlag, Usage: "Alias for the Cloudflare API token", Sources: cli.EnvVars("CLOUDFLARE_API_TOKEN")},
		&cli.StringFlag{Name: serviceDomainFlag, Usage: "Public hostname to expose (e.g. mcp.example.com)", Sources: cli.EnvVars("MCP_DOMAIN")},
		&cli.StringFlag{Name: serviceTunnelNameFlag, Usage: "Cloudflare tunnel resource name (default: pinner-mcp)", Sources: cli.EnvVars("MCP_TUNNEL_NAME")},
		&cli.StringFlag{Name: serviceAuthTokenFlag, Usage: "Shared secret authorizing the public MCP endpoint", Sources: cli.EnvVars("MCP_AUTH_TOKEN")},
		&cli.StringFlag{Name: serviceEnvFileFlag, Usage: "MCP service environment file"},
	}
}

// printTunnelStatus renders the provisioned tunnel state (without secrets).
func printTunnelStatus(s *CloudflareTunnelState) {
	fmt.Printf("Provider:  %s\n", s.Provider)
	fmt.Printf("Tunnel ID: %s\n", s.TunnelID)
	fmt.Printf("Hostname:  %s\n", s.Hostname)
	fmt.Printf("Token:     provisioned (scoped to this tunnel)\n")
}
