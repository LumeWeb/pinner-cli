package cli

import (
	"context"
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// quotaStatusMap converts a quota dimension's primitive fields into a JSON map
// for the pinner://account/status resource. It takes primitives because the
// SDK surfaces these under an internal (pinner-unimportable) struct type.
func quotaStatusMap(used int, limit, remaining, reserved, threshold *int, percentage int) map[string]any {
	m := map[string]any{
		"used":       used,
		"percentage": percentage,
	}
	if limit != nil {
		m["limit"] = *limit
	}
	if remaining != nil {
		m["remaining"] = *remaining
	}
	if reserved != nil {
		m["reserved"] = *reserved
	}
	if threshold != nil {
		m["threshold"] = *threshold
	}
	return m
}

// hasQuotaRemaining reports whether an explicit remaining-allowance bound
// proves usable. A nil remaining (omitted) must NOT count as covered: the
// schema has no unlimited marker and zero-quota accounts report all nil, so
// coverage requires an explicit positive remaining.
func hasQuotaRemaining(remaining *int) bool {
	return remaining != nil && *remaining > 0
}

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

func (a *accountStatusAdapter) Quota(ctx context.Context) map[string]any {
	if a.auth == nil {
		return nil
	}
	q, err := a.auth.GetQuota(ctx)
	if err != nil {
		// Best-effort: the resource handler tolerates a missing/nil quota and
		// still reports auth + config. Surface the error so the agent can see
		// why quota is unavailable rather than silently omitting it.
		return map[string]any{"error": err.Error()}
	}
	if q == nil {
		return nil
	}
	return map[string]any{
		"has_quota": hasQuotaRemaining(q.Upload.Remaining) || hasQuotaRemaining(q.Download.Remaining) || hasQuotaRemaining(q.Storage.Remaining),
		"upload":    quotaStatusMap(q.Upload.Used, q.Upload.Limit, q.Upload.Remaining, q.Upload.Reserved, q.Upload.Threshold, q.Upload.Percentage),
		"download":  quotaStatusMap(q.Download.Used, q.Download.Limit, q.Download.Remaining, q.Download.Reserved, q.Download.Threshold, q.Download.Percentage),
		"storage":   quotaStatusMap(q.Storage.Used, q.Storage.Limit, q.Storage.Remaining, q.Storage.Reserved, q.Storage.Threshold, q.Storage.Percentage),
	}
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
	item, ok, err := catalog.ScanPages(ctx, w.ws,
		func(item ipfs.WebsiteItem) (bool, error) {
			return dnsname.Equal(item.Domain, domain), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list websites: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("website not found for domain %q", domain)
	}
	return &item, nil
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

func (w *websitesResourceAdapter) ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return w.ws.ListPlatformDomains(ctx)
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
