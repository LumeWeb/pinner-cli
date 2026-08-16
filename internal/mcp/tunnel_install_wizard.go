package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	// Route the hostname at the tunnel, capturing the created DNS record ID so
	// a later failure can roll the route back too (otherwise the proxied CNAME
	// stays behind and blocks clean re-provisioning).
	// Use a cancellation-exempt context for rollback deletes: if the failure we
	// are cleaning up after was itself a context cancellation (e.g. SIGINT), the
	// delete calls must still run, or the billed tunnel + CNAME are orphaned.
	cleanupCtx := context.WithoutCancel(ctx)
	recordID, err := c.CreateDNSRoute(ctx, account, zone, hostname, rec.ID)
	if err != nil {
		_ = c.DeleteTunnel(cleanupCtx, account, rec.ID)
		return nil, err
	}
	// cleanup tears down both the DNS route and the tunnel so a mid-provision
	// failure (token fetch, state save) never orphans a billed Cloudflare
	// tunnel or leaves its hostname occupied.
	cleanup := func() {
		_ = c.DeleteDNSRoute(cleanupCtx, zone, recordID)
		_ = c.DeleteTunnel(cleanupCtx, account, rec.ID)
	}
	token, err := c.GetTunnelToken(ctx, account, rec.ID)
	if err != nil {
		cleanup()
		return nil, err
	}
	state := &CloudflareTunnelState{
		Provider:    TunnelProviderCloudflared,
		AccountID:   rec.AccountID,
		TunnelID:    rec.ID,
		TunnelName:  rec.Name,
		Secret:      rec.Secret,
		Token:       token,
		Hostname:    hostname,
		ZoneID:      zone.ID,
		DNSRecordID: recordID,
	}
	if err := SaveCloudflareTunnelState(state); err != nil {
		cleanup()
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
	// Both --cloudflare-api-token and its alias --api-key source the same
	// CLOUDFLARE_API_TOKEN env var, so when that var is present cmd.String()
	// returns the env value for both even if neither flag was typed on the
	// CLI. An explicitly-passed flag must still win over an ambient (possibly
	// stale) token, so we compare each flag value against the raw env var: a
	// value that differs from the env is a genuine CLI override; otherwise we
	// fall back to the env value (identical either way). Precedence:
	// --cloudflare-api-token > --api-key > env.
	envToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	apiToken := strings.TrimSpace(cmd.String(cloudflareTokenFlag))
	apiKey := strings.TrimSpace(cmd.String(serviceApiKeyFlag))
	// A CLI-typed value differs from the ambient env; honor it first.
	if apiToken != "" && apiToken != envToken {
		return apiToken, nil
	}
	if apiKey != "" && apiKey != envToken {
		return apiKey, nil
	}
	// Nothing differs from env; any of the flag/env values (they are the same)
	// is an acceptable token.
	if apiToken != "" {
		return apiToken, nil
	}
	if apiKey != "" {
		return apiKey, nil
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
// shared by the tunnel install wizard and the service install wizard. It also
// returns the Cloudflare API token used, so a caller can authenticate a clean-up
// client (the tunnel-scoped run token is not a valid API token).
func provisionCloudflaredForWizard(ctx context.Context, cmd *cli.Command, cfNew func(string) (cloudflare.Client, error)) (*CloudflareTunnelState, string, error) {
	apiToken, err := obtainCloudflareAPIToken(ctx, cmd)
	if err != nil {
		return nil, "", err
	}
	c, err := cfNew(apiToken)
	if err != nil {
		return nil, "", err
	}
	if err := c.VerifyToken(ctx); err != nil {
		return nil, "", err
	}
	account, err := resolveCloudflareAccount(ctx, c)
	if err != nil {
		return nil, "", err
	}
	hostname := strings.TrimSpace(cmd.String(serviceDomainFlag))
	if hostname == "" {
		hostname, err = textUI{}.Text("Public hostname to expose (e.g. mcp.example.com)")
		if err != nil {
			return nil, "", err
		}
		hostname = strings.TrimSpace(hostname)
	}
	if hostname == "" {
		return nil, "", errors.New("a public hostname is required")
	}
	zone, err := c.FindZone(ctx, account, hostname)
	if err != nil {
		return nil, "", fmt.Errorf("resolve DNS zone for %q: %w (is the domain's nameservers pointing at Cloudflare?)", hostname, err)
	}
	name := strings.TrimSpace(cmd.String(serviceTunnelNameFlag))
	if name == "" {
		name = "pinner-mcp"
	}
	// Refuse to double-provision: if a tunnel is already provisioned, surface
	// it so the user can tear it down instead of silently orphaning the prior
	// tunnel + DNS CNAME (and overwriting tunnel-state.json) on a re-run.
	if prior, lerr := LoadCloudflareTunnelState(); lerr == nil && prior != nil {
		return nil, "", fmt.Errorf("a tunnel (%s, hostname %s) is already provisioned; run `pinner mcp tunnel status`, then clean it up before re-provisioning", prior.TunnelID, prior.Hostname)
	}
	fmt.Printf("Provisioning Cloudflare tunnel %q for %q ...\n", name, hostname)
	state, err := provisionCloudflareTunnel(ctx, c, account, zone, name, hostname)
	if err != nil {
		return nil, "", fmt.Errorf("provision cloudflare tunnel: %w", err)
	}
	fmt.Printf("Tunnel provisioned (id %s).\n", state.TunnelID)
	return state, apiToken, nil
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
	state, apiToken, err := provisionCloudflaredForWizard(ctx, cmd, cfNew)
	if err != nil {
		return err
	}

	// 6. Ensure the cloudflared binary is available. Capture the resolved
	// path because a bin-dir download install is NOT placed on PATH: the
	// follow-up `pinner mcp service install` uses resolveCloudflaredPath
	// (bin-dir aware), but the user should still be told where it landed.
	cfBin := ""
	if p, err := ensureCloudflaredBinary(ctx); err != nil {
		fmt.Printf("warning: %v\n", err)
	} else {
		cfBin = p
	}

	// 7. Write the service environment file (provider/domain/tunnel-token).
	envFile := cmd.String(serviceEnvFileFlag)
	if envFile == "" {
		envFile, err = resolveServiceEnvFile(cmd)
		if err != nil {
			return err
		}
	}
	// The public tunnel requires a shared secret protecting the exposed MCP
	// endpoint; validateServiceEnvironment refuses a cloudflared env file
	// without MCP_AUTH_TOKEN. Mirror the service-install wizard and prompt for
	// a masked token when none was given via --auth-token, aborting before the
	// env file is written (and thus before a later `pinner mcp service
	// install` would reject it) if it cannot be obtained.
	authToken := strings.TrimSpace(cmd.String(serviceAuthTokenFlag))
	if authToken == "" {
		secret, err := textUI{mask: "*"}.Text("Shared secret authorizing the public MCP endpoint (required)")
		if err != nil {
			return err
		}
		authToken = strings.TrimSpace(secret)
	}
	if authToken == "" {
		return errors.New("an MCP auth token is required for the public tunnel; pass --auth-token")
	}
	env := ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": string(TunnelProviderCloudflared),
		"MCP_DOMAIN":          state.Hostname,
		"MCP_TUNNEL_TOKEN":    state.Token,
		"MCP_AUTH_TOKEN":      authToken,
	}
	if err := WriteServiceEnvironment(envFile, env); err != nil {
		// The tunnel + DNS route were provisioned and persisted above; roll
		// them both back so a failed env write does not leave an orphaned,
		// billed tunnel pair behind (re-running the wizard would otherwise
		// double it). Authenticate the clean-up client with the original Cloudflare
		// API token — the tunnel-scoped run token is not a valid API token.
		if st, lerr := LoadCloudflareTunnelState(); lerr == nil && st != nil {
			if c, cerr := cloudflare.New(apiToken); cerr == nil {
				if st.DNSRecordID != "" {
					_ = c.DeleteDNSRoute(context.WithoutCancel(ctx), cloudflare.Zone{ID: st.ZoneID}, st.DNSRecordID)
				}
				_ = c.DeleteTunnel(context.WithoutCancel(ctx), cloudflare.Account{ID: st.AccountID}, st.TunnelID)
			}
			if p, perr := tunnelStatePath(); perr == nil {
				_ = os.Remove(p)
			}
		}
		return fmt.Errorf("write MCP service environment file: %w", err)
	}
	fmt.Printf("Wrote MCP service environment file %s.\n", envFile)
	if cfBin != "" {
		// If cloudflared landed in the packed pinner bin dir rather than on
		// PATH, tell the user: service install resolves it via the bin dir,
		// but the end user may want it reachable as a plain command.
		fmt.Printf("cloudflared binary at %s\n", cfBin)
	}
	fmt.Printf("Run `pinner mcp service install` to register it, or `pinner mcp --tunnel cloudflared --auth-token <secret>` to run it now.\n")
	return nil
}
