// Package cloudflare wraps the official Cloudflare API (cloudflare-go) as a
// management library for Cloudflare Tunnels. It is the "library" side of the
// cloudflared integration: everything except the raw QUIC-edge dataplane
// connection (which still uses the cloudflared binary) is managed here —
// verifying a scoped API token, resolving the account and DNS zone, creating a
// named tunnel, routing DNS, and fetching the per-tunnel credential the
// cloudflared binary consumes.
//
// All methods are behind the Client interface so callers (the tunnel install /
// service install wizards) can inject a fake in tests and run live-account
// empirical verification in production, matching pinner-cli's testing
// preference.
package cloudflare

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go"
)

// Account is a Cloudflare account the token can act on.
type Account struct {
	ID   string
	Name string
}

// Zone is a DNS zone that hosts the tunnel's public hostname.
type Zone struct {
	ID   string
	Name string
}

// TunnelRecord is a provisioned named tunnel plus the credentials needed to
// run it (the credentials-file secret and the scoped run token) and the
// account that owns it (the credentials file's AccountTag).
type TunnelRecord struct {
	// AccountID is the Cloudflare account tag that owns the tunnel; it is the
	// AccountTag field of a cloudflared credentials file.
	AccountID string
	// ID is the tunnel UUID.
	ID string
	// Name is the tunnel name.
	Name string
	// Secret is the tunnel secret (>= 32 bytes base64) required by a
	// locally-managed tunnel's credentials file.
	Secret string
	// Token is the scoped run JWT for the tunnel.
	Token string
}

// Client is the interface for Cloudflare tunnel management. It is deliberately
// narrow so tests use a fake and only the real implementation touches the SDK.
type Client interface {
	// VerifyToken confirms the API token is valid and can list accounts at
	// least (a superset check; individual tunnel/DNS perms are validated when
	// the corresponding call is made). Returns a readable error if the token
	// is invalid or has no usable scope.
	VerifyToken(ctx context.Context) error
	// ListAccounts returns the accounts the token can act on.
	ListAccounts(ctx context.Context) ([]Account, error)
	// FindZone resolves the zone that hosts domain (the tunnel's public
	// hostname) within account.
	FindZone(ctx context.Context, account Account, domain string) (Zone, error)
	// CreateTunnel creates a named tunnel in account and returns the record
	// (including the generated credentials-file secret) needed to run it.
	CreateTunnel(ctx context.Context, account Account, name string) (TunnelRecord, error)
	// GetTunnelToken returns the scoped JWT used to run a specific tunnel.
	GetTunnelToken(ctx context.Context, account Account, tunnelID string) (string, error)
	// CreateDNSRoute points hostname (a subdomain of a zone in account) at the
	// named tunnel, proxied through Cloudflare. It returns the created DNS
	// record ID so a caller can remove the route again on failure.
	CreateDNSRoute(ctx context.Context, account Account, zone Zone, hostname, tunnelID string) (string, error)
	// DeleteDNSRoute removes a DNS record (by its record ID) from zone. Used to
	// best-effort clean up a route created mid-provision when a later step fails.
	DeleteDNSRoute(ctx context.Context, zone Zone, recordID string) error
	// DeleteTunnel removes a named tunnel from account. Used to best-effort
	// clean up a tunnel created mid-provision when a later step fails.
	DeleteTunnel(ctx context.Context, account Account, tunnelID string) error
}

// cfClient is the real Client backed by the cloudflare-go SDK.
type cfClient struct {
	api *cf.API
}

// New returns a Client authenticated with the given API token. An empty token
// is rejected up front with a clear error.
func New(apiToken string) (Client, error) {
	if apiToken == "" {
		return nil, errors.New("cloudflare API token is empty")
	}
	api, err := cf.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, fmt.Errorf("create cloudflare client: %w", err)
	}
	return &cfClient{api: api}, nil
}

// Ensure cfClient satisfies Client at compile time.
var _ Client = (*cfClient)(nil)

// VerifyToken implements Client.
func (c *cfClient) VerifyToken(ctx context.Context) error {
	if _, _, err := c.api.Accounts(ctx, cf.AccountsListParams{}); err != nil {
		return fmt.Errorf("cloudflare API token is invalid or cannot list accounts: %w", err)
	}
	return nil
}

// ListAccounts implements Client.
func (c *cfClient) ListAccounts(ctx context.Context) ([]Account, error) {
	accounts, _, err := c.api.Accounts(ctx, cf.AccountsListParams{})
	if err != nil {
		return nil, fmt.Errorf("list cloudflare accounts: %w", err)
	}
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, Account{ID: a.ID, Name: a.Name})
	}
	return out, nil
}

// zoneHostnameMatches reports whether the tunnel hostname `domain` belongs to
// the DNS zone `zoneName`. The passed domain is the public hostname (often a
// subdomain like mcp.example.com) while the matching Cloudflare zone is the
// parent (example.com), so we accept either an exact zone-name match or a
// hostname nested beneath it, ignoring case and an optional http(s):// scheme.
func zoneHostnameMatches(zoneName, domain string) bool {
	zn := strings.ToLower(zoneName)
	dn := strings.ToLower(domain)
	// Strip a leading scheme case-insensitively (after lowercasing) so both
	// "https://mcp.example.com" and "HTTP://mcp.example.com" resolve to the
	// same zone as the bare hostname.
	for _, prefix := range []string{"https://", "http://"} {
		dn = strings.TrimPrefix(dn, prefix)
	}
	return dn == zn || strings.HasSuffix(dn, "."+zn)
}

// FindZone implements Client. It scopes the lookup to the selected account so
// a token with access to multiple accounts cannot resolve the tunnel's public
// zone to a different account than the one the tunnel was created in.
func (c *cfClient) FindZone(ctx context.Context, account Account, domain string) (Zone, error) {
	// ListZonesContext auto-paginates internally (it fans out across pages when
	// TotalPages >= 2), so a single call already returns every zone the token
	// can see. The account.ID filter below then guarantees the match is scoped
	// to the account the tunnel was provisioned in.
	zones, err := c.api.ListZonesContext(ctx)
	if err != nil {
		return Zone{}, fmt.Errorf("resolve zone for %q: %w", domain, err)
	}
	for _, z := range zones.Result {
		if z.Account.ID == account.ID && zoneHostnameMatches(z.Name, domain) {
			return Zone{ID: z.ID, Name: z.Name}, nil
		}
	}
	return Zone{}, fmt.Errorf("zone %q not found in account %s", domain, account.ID)
}

// CreateTunnel implements Client. It generates a fresh tunnel secret (used by
// the tunnel's credentials file) rather than requiring the caller to supply
// one, so the minimal-permission wizard token stays single-purpose.
func (c *cfClient) CreateTunnel(ctx context.Context, account Account, name string) (TunnelRecord, error) {
	secret, err := NewTunnelSecret()
	if err != nil {
		return TunnelRecord{}, fmt.Errorf("generate tunnel secret: %w", err)
	}
	// The SDK enforces a non-empty secret and, historically, a locally-managed
	// tunnel. Use ConfigSrc "local" so the tunnel is managed by a local
	// config.yml + credentials file (the model that supports a dynamically
	// bound local origin port for the MCP server), rather than by the
	// dashboard. We still mint and persist the scoped run token for flexible
	// credentials delivery.
	params := cf.TunnelCreateParams{
		Name:      name,
		Secret:    secret,
		ConfigSrc: "local",
	}
	t, err := c.api.CreateTunnel(ctx, cf.AccountIdentifier(account.ID), params)
	if err != nil {
		return TunnelRecord{}, fmt.Errorf("create cloudflare tunnel %q: %w", name, err)
	}
	return TunnelRecord{AccountID: account.ID, ID: t.ID, Name: t.Name, Secret: secret}, nil
}

// GetTunnelToken implements Client.
func (c *cfClient) GetTunnelToken(ctx context.Context, account Account, tunnelID string) (string, error) {
	token, err := c.api.GetTunnelToken(ctx, cf.AccountIdentifier(account.ID), tunnelID)
	if err != nil {
		return "", fmt.Errorf("get cloudflare tunnel token for %q: %w", tunnelID, err)
	}
	return token, nil
}

// CreateDNSRoute implements Client. It creates a proxied CNAME pointing
// hostname at the tunnel's <tunnel-id>.cfargotunnel.com target and returns the
// created DNS record ID so the caller can roll the route back on failure.
func (c *cfClient) CreateDNSRoute(ctx context.Context, account Account, zone Zone, hostname, tunnelID string) (string, error) {
	proxied := true
	params := cf.CreateDNSRecordParams{
		Type:    "CNAME",
		Name:    hostname,
		Content: tunnelID + ".cfargotunnel.com",
		Proxied: &proxied,
	}
	record, err := c.api.CreateDNSRecord(ctx, cf.ZoneIdentifier(zone.ID), params)
	if err != nil {
		return "", fmt.Errorf("create DNS route %q -> tunnel %q: %w", hostname, tunnelID, err)
	}
	return record.ID, nil
}

// DeleteDNSRoute implements Client. It removes a DNS record by ID from a zone
// so a mid-provision failure never leaves the hostname occupied by a proxied
// CNAME pointing at a deleted tunnel.
func (c *cfClient) DeleteDNSRoute(ctx context.Context, zone Zone, recordID string) error {
	if err := c.api.DeleteDNSRecord(ctx, cf.ZoneIdentifier(zone.ID), recordID); err != nil {
		return fmt.Errorf("delete DNS route record %q in zone %q: %w", recordID, zone.Name, err)
	}
	return nil
}

// DeleteTunnel implements Client. It best-effort removes a named tunnel so a
// mid-provision failure (DNS, token, or state write) never orphans an unused
// Cloudflare tunnel resource.
func (c *cfClient) DeleteTunnel(ctx context.Context, account Account, tunnelID string) error {
	if err := c.api.DeleteTunnel(ctx, cf.AccountIdentifier(account.ID), tunnelID); err != nil {
		return fmt.Errorf("delete cloudflare tunnel %q: %w", tunnelID, err)
	}
	return nil
}

// NewTunnelSecret returns a cryptographically random 32-byte secret base64
// encoded, as required by the Cloudflare tunnel create API (>= 32 bytes,
// base64).
func NewTunnelSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
