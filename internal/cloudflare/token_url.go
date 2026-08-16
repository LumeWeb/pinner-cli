package cloudflare

import (
	"encoding/json"
	"net/url"
)

// Permission scope for the tunnel-management API token. The dashboard
// deep-link pre-fills these; they are the minimal set needed to provision a
// named tunnel and its DNS route:
//
//   - argotunnel:edit — create/delete Cloudflare Tunnels
//   - dns:edit        — create the CNAME record routing the hostname to the tunnel
//   - zone:read       — resolve the zone that hosts the tunnel domain
type tokenPermission struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// tunnelPermissions is the exact scope set the wizard requests. Cloudflare
// does not support minting a token scoped to a single tunnel via deep-link
// (token permissions are account/zone scoped); the truly tunnel-scoped
// credential is the per-tunnel JWT we provision and persist separately.
var tunnelPermissions = []tokenPermission{
	{Key: "argotunnel", Type: "edit"},
	{Key: "dns", Type: "edit"},
	{Key: "zone", Type: "read"},
}

// BuildTokenTemplateURL returns a Cloudflare dashboard URL that, when opened,
// pre-fills the API token creation page with the tunnel-management scope and
// the given token name. The user completes and validates the form in the
// dashboard, then pastes the generated token back into the wizard.
//
// URL shape (Cloudflare "API token template URLs"):
//
//	https://dash.cloudflare.com/profile/api-tokens?permissionGroupKeys=[...]&accountId=*&zoneId=all&name=<name>
func BuildTokenTemplateURL(tokenName string) string {
	base := "https://dash.cloudflare.com/profile/api-tokens"
	q := url.Values{}
	b, _ := json.Marshal(tunnelPermissions)
	q.Set("permissionGroupKeys", string(b))
	q.Set("accountId", "*")
	q.Set("zoneId", "all")
	if tokenName != "" {
		q.Set("name", tokenName)
	}
	return base + "?" + q.Encode()
}
