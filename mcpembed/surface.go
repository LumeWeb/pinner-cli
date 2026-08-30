// Package mcpembed is the public, deliberately-tiny embedding façade for
// running Pinner as an MCP server inside another host (e.g. the LumeWeb
// Portal). It wraps go.lumeweb.com/pinner-cli/internal/mcp — the canonical
// catalog/MCP implementation the CLI uses — behind a stable, small API, so a
// hosted product is a different ASSEMBLY of the same implementation rather
// than a fork.
//
// mcpembed exposes the hosted surface (account/subscription and IPFS/websites/
// DNS; never the Sia vault or portal admin), the auth abstraction seams
// (CredentialResolver and OAuthHandler, so the Portal can route the MCP OAuth
// backend to its own IdP instead of the CLI's), and New(...) -> http.Handler.
package mcpembed

import (
	"go.lumeweb.com/pinner-cli/internal/mcp"
)

// Surface declares which operation domains/tool families the embedded hosted
// server exposes. Unlike the internal Surface, the zero value here means
// "nothing enabled" — callers opt in explicitly (typically via SurfaceHosted).
//
// The Sia vault and portal-admin domains are intentionally not represented:
// a hosted embed never exposes them.
type Surface struct {
	// Account enables account, subscription, auth, and API-key operations.
	Account bool
	// Pins enables the IPFS pinning operations.
	Pins bool
	// Websites enables IPFS website publishing operations.
	Websites bool
	// DNS enables the DNS zone/record operations.
	DNS bool
	// IPNS enables the IPNS key/publish operations.
	IPNS bool
	// ENS enables the ENS/onchain pointing operations.
	ENS bool
	// Operations enables the operations-status operations.
	Operations bool
	// Upload enables the IPFS upload/download tool family.
	Upload bool
}

// SurfaceHosted is the standard hosted surface: account/subscription plus the
// full IPFS/websites/DNS/IPNS/ENS/operations set (no vault, no admin).
var SurfaceHosted = Surface{
	Account:    true,
	Pins:       true,
	Websites:   true,
	DNS:        true,
	IPNS:       true,
	ENS:        true,
	Operations: true,
	Upload:     true,
}

// IsZero reports whether the surface has every flag disabled (the empty
// value). mcpembed.New treats a zero Surface as SurfaceHosted.
func (s Surface) IsZero() bool {
	return s == (Surface{})
}

// toInternal maps the public hosted surface onto the internal Surface used by
// the MCP implementation. The vault and admin domains are always disabled.
func (s Surface) toInternal() mcp.Surface {
	return mcp.Surface{
		Account:    s.Account,
		Pins:       s.Pins,
		Websites:   s.Websites,
		DNS:        s.DNS,
		IPNS:       s.IPNS,
		ENS:        s.ENS,
		Operations: s.Operations,
		Upload:     s.Upload,
		Vault:      false,
		Admin:      false,
	}
}
