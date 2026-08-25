package cli

import (
	"context"
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// accountStatusAdapter wraps cli.AuthService and config to satisfy the MCP
// AccountStatusProvider interface. It holds cfgMgr (not a config snapshot)
// so IsAuthenticated and APIKey reflect latest auth state at request time.
type accountStatusAdapter struct {
	cfgMgr config.Manager
	auth   AuthService
}

func (a *accountStatusAdapter) IsAuthenticated() bool {
	if a.cfgMgr == nil {
		return false
	}
	return a.cfgMgr.Config().IsAuthenticated()
}

func (a *accountStatusAdapter) AuthStatus(ctx context.Context) error {
	if a.auth == nil {
		return fmt.Errorf("auth service not configured")
	}
	_, err := a.auth.Status(ctx)
	return err
}

func (a *accountStatusAdapter) APIKey() string {
	if a.cfgMgr == nil {
		return ""
	}
	t := a.cfgMgr.Config().AuthToken
	if t == "" {
		return ""
	}
	if len(t) <= 4 {
		return "****"
	}
	return "****" + t[len(t)-4:]
}

func (a *accountStatusAdapter) Quota(_ context.Context) map[string]any {
	// TODO: wire quota via portal account API; resource handler tolerates nil.
	return nil
}

func (a *accountStatusAdapter) ConfigSummary() map[string]any {
	if a.cfgMgr == nil {
		return nil
	}
	cfg := a.cfgMgr.Config()
	summary := map[string]any{
		"base_endpoint":   cfg.GetBaseEndpoint(),
		"secure":          cfg.Secure,
		"max_retries":     cfg.MaxRetries,
		"memory_limit_mb": cfg.MemoryLimit,
	}
	if cfg.GatewayEndpoint != "" {
		summary["gateway_endpoint"] = cfg.GatewayEndpoint
	}
	return summary
}

// websitesResourceAdapter wraps cli.WebsitesService to satisfy the MCP
// WebsitesResourceProvider interface. The cli service resolves by ID via Get,
// so GetByDomain does a List + domain match.
type websitesResourceAdapter struct {
	ws WebsitesService
}

func (w *websitesResourceAdapter) GetByDomain(ctx context.Context, domain string) (*ipfs.WebsiteItem, error) {
	websites, err := w.ws.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list websites: %w", err)
	}
	for i := range websites {
		if websites[i].Domain == domain {
			return &websites[i], nil
		}
	}
	return nil, fmt.Errorf("website not found for domain %q", domain)
}

func (w *websitesResourceAdapter) GetByID(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	return w.ws.Get(ctx, id)
}

func (w *websitesResourceAdapter) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	return w.ws.Validate(ctx, id)
}

func (w *websitesResourceAdapter) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	return w.ws.GetConfig(ctx)
}

func (w *websitesResourceAdapter) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	return w.ws.CheckPlatformDomainAvailability(ctx, label)
}

// pinStatusAdapter wraps cli.PinningService to satisfy the MCP
// PinningProvider interface for the "Create a Pin" app's app-only status
// helper. It adapts the CLI's Status(cid, watch=false) into the SDK-neutral
// PinStatusView the mcp package renders.
type pinStatusAdapter struct {
	pins PinningService
}

func (p *pinStatusAdapter) PinStatus(ctx context.Context, cid string) (apps.PinStatusView, error) {
	status, err := p.pins.Status(ctx, cid, false)
	if err != nil {
		return apps.PinStatusView{}, err
	}
	if status == nil {
		return apps.PinStatusView{CID: cid}, nil
	}
	return apps.PinStatusView{CID: status.CID, Status: status.Status}, nil
}
