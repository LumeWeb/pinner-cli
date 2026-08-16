package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/cloudflare"
	"go.lumeweb.com/pinner-cli/internal/urlopen"
)

// provisionCloudflareTunnel creates a named Cloudflare tunnel, routes its DNS,
// and fetches the scoped run credential, returning the persisted tunnel state.
// It is the pure provisioning core shared by the tunnel install wizard and the
// service install wizard (both wrap it with their own prompting).
//
// name is the tunnel resource name; hostname is the public hostname to expose.
// account is the Cloudflare account the token is scoped to; zone is the DNS
// zone hosting hostname.
func provisionCloudflareTunnel(ctx context.Context, c cloudflare.Client, account cloudflare.Account, zone cloudflare.Zone, name, hostname string) (*CloudflareTunnelState, error) {
	if c == nil {
		return nil, errors.New("cloudflare client is nil")
	}
	rec, err := c.CreateTunnel(ctx, account, name)
	if err != nil {
		return nil, err
	}
	if err := c.CreateDNSRoute(ctx, account, zone, hostname, rec.ID); err != nil {
		// Best-effort delete the orphaned tunnel so a mid-provision failure
		// (DNS, token, save) never leaks an unused Cloudflare tunnel.
		_ = c.DeleteTunnel(ctx, account, rec.ID)
		return nil, err
	}
	token, err := c.GetTunnelToken(ctx, account, rec.ID)
	if err != nil {
		_ = c.DeleteTunnel(ctx, account, rec.ID)
		return nil, err
	}
	state := &CloudflareTunnelState{
		Provider:   TunnelProviderCloudflared,
		AccountID:  rec.AccountID,
		TunnelID:   rec.ID,
		TunnelName: rec.Name,
		Secret:     rec.Secret,
		Token:      token,
		Hostname:   hostname,
	}
	if err := SaveCloudflareTunnelState(state); err != nil {
		return nil, err
	}
	return state, nil
}

// resolveCloudflareAccount lists the accounts a token can act on and picks one,
// prompting when there are several and auto-selecting when there is exactly one.
func resolveCloudflareAccount(ctx context.Context, c cloudflare.Client) (cloudflare.Account, error) {
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return cloudflare.Account{}, err
	}
	switch len(accounts) {
	case 0:
		return cloudflare.Account{}, errors.New("the Cloudflare API token has no accessible accounts")
	case 1:
		return accounts[0], nil
	default:
		options := make([]string, len(accounts))
		for i, a := range accounts {
			options[i] = a.Name
		}
		_, choice, err := selectUI{}.Select("Cloudflare account", options)
		if err != nil {
			return cloudflare.Account{}, err
		}
		for i, a := range accounts {
			if a.Name == choice {
				return accounts[i], nil
			}
		}
		return cloudflare.Account{}, errors.New("selected account not found")
	}
}

// obtainCloudflareAPIToken returns a scoped Cloudflare API token, either from
// flags/env or by deep-linking the user to the dashboard template URL and
// having them paste the generated token back. It errors in non-interactive
// mode when no token is supplied.
func obtainCloudflareAPIToken(ctx context.Context, cmd *cli.Command) (string, error) {
	// Read the dedicated Cloudflare token flag (sourced only from
	// CLOUDFLARE_API_TOKEN) first, then the --api-key alias, so a different
	// provider's control-plane credential can never be routed to Cloudflare.
	apiToken := strings.TrimSpace(cmd.String(cloudflareTokenFlag))
	if apiToken == "" {
		apiToken = strings.TrimSpace(cmd.String(serviceApiKeyFlag))
	}
	if apiToken != "" {
		return apiToken, nil
	}
	tokenURL := cloudflare.BuildTokenTemplateURL("Pinner MCP Tunnel")
	if wizard.NonInteractive || cmd.Bool("agent") {
		return "", errors.New("non-interactive mode: run with --api-key <CLOUDFLARE_API_TOKEN> or CLOUDFLARE_API_TOKEN set")
	}
	fmt.Printf("To provision a Cloudflare tunnel, pinner needs a scoped API token.\n")
	fmt.Printf("Open this URL in your browser to create one (Tunnel:Edit, DNS:Edit, Zone:Read):\n\n  %s\n\n", tokenURL)
	if err := urlopen.Open(tokenURL); err != nil {
		fmt.Printf("(could not auto-open the browser: %v)\n", err)
	}
	key, err := textUI{mask: "*"}.Text("Paste the generated Cloudflare API token")
	if err != nil {
		return "", err
	}
	apiToken = strings.TrimSpace(key)
	if apiToken == "" {
		return "", errors.New("a Cloudflare API token is required")
	}
	return apiToken, nil
}

// provisionCloudflaredForWizard deep-links a token, resolves the account/zone,
// provisions the tunnel + DNS route, and persists the tunnel state. It is
// shared by the tunnel install wizard and the service install wizard.
func provisionCloudflaredForWizard(ctx context.Context, cmd *cli.Command, cfNew func(string) (cloudflare.Client, error)) (*CloudflareTunnelState, error) {
	apiToken, err := obtainCloudflareAPIToken(ctx, cmd)
	if err != nil {
		return nil, err
	}
	c, err := cfNew(apiToken)
	if err != nil {
		return nil, err
	}
	if err := c.VerifyToken(ctx); err != nil {
		return nil, err
	}
	account, err := resolveCloudflareAccount(ctx, c)
	if err != nil {
		return nil, err
	}
	hostname := strings.TrimSpace(cmd.String(serviceDomainFlag))
	if hostname == "" {
		hostname, err = textUI{}.Text("Public hostname to expose (e.g. mcp.example.com)")
		if err != nil {
			return nil, err
		}
		hostname = strings.TrimSpace(hostname)
	}
	if hostname == "" {
		return nil, errors.New("a public hostname is required")
	}
	zone, err := c.FindZone(ctx, account, hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve DNS zone for %q: %w (is the domain's nameservers pointing at Cloudflare?)", hostname, err)
	}
	name := strings.TrimSpace(cmd.String(serviceTunnelNameFlag))
	if name == "" {
		name = "pinner-mcp"
	}
	fmt.Printf("Provisioning Cloudflare tunnel %q for %q ...\n", name, hostname)
	state, err := provisionCloudflareTunnel(ctx, c, account, zone, name, hostname)
	if err != nil {
		return nil, fmt.Errorf("provision cloudflare tunnel: %w", err)
	}
	fmt.Printf("Tunnel provisioned (id %s).\n", state.TunnelID)
	return state, nil
}

// runTunnelInstallWizard drives the interactive `pinner mcp tunnel install`
// wizard. It guides the user through getting a scoped Cloudflare API token via
// a dashboard deep-link, validates it, provisions the named tunnel + DNS route,
// ensures the cloudflared binary is present, and writes the service environment.
func runTunnelInstallWizard(ctx context.Context, cmd *cli.Command, ui *serviceInstallWizardUI, cfNew func(string) (cloudflare.Client, error)) error {
	// 1. Provider selection — cloudflared is the fully-implemented installer
	// this iteration.
	provider := TunnelProviderCloudflared
	if v := strings.TrimSpace(cmd.String(serviceTunnelFlag)); v != "" {
		p, err := parseTunnelProvider(v)
		if err != nil {
			return err
		}
		provider = p
	}
	if provider != TunnelProviderCloudflared {
		return fmt.Errorf("tunnel install currently supports only cloudflared (got %q); use `pinner mcp service install` for other providers", provider)
	}

	// 2-5. Deep-link token, verify, account, zone, provision. Shared with the
	// service install wizard.
	state, err := provisionCloudflaredForWizard(ctx, cmd, cfNew)
	if err != nil {
		return err
	}

	// 6. Ensure the cloudflared binary is available.
	if _, err := ensureCloudflaredBinary(ctx); err != nil {
		fmt.Printf("warning: %v\n", err)
	}

	// 7. Write the service environment file (provider/domain/tunnel-token).
	envFile := cmd.String(serviceEnvFileFlag)
	if envFile == "" {
		envFile, err = resolveServiceEnvFile(cmd)
		if err != nil {
			return err
		}
	}
	authToken := strings.TrimSpace(cmd.String(serviceAuthTokenFlag))
	env := ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": string(TunnelProviderCloudflared),
		"MCP_DOMAIN":          state.Hostname,
		"MCP_TUNNEL_TOKEN":    state.Token,
	}
	if authToken != "" {
		env["MCP_AUTH_TOKEN"] = authToken
	}
	if err := WriteServiceEnvironment(envFile, env); err != nil {
		return fmt.Errorf("write MCP service environment file: %w", err)
	}
	fmt.Printf("Wrote MCP service environment file %s.\n", envFile)
	fmt.Printf("Run `pinner mcp service install` to register it, or `pinner mcp --tunnel cloudflared --auth-token <secret>` to run it now.\n")
	return nil
}
