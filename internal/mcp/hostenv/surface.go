package hostenv

// Surface describes which operation domains and tool families a Pinner MCP
// server registers. It is the single source of truth that makes "hosted mode"
// (a Portal-embedded server exposing account/subscription and IPFS/websites/
// DNS only, with no Sia vault) a different ASSEMBLY of the same implementation
// rather than a fork or a second MCP implementation.
//
// The surface is a server-construction-time property. It is declared on the
// resolved PlatformProfile so the whole profile-aware surface (catalog ops,
// tool registration, MCP Apps, resources, prompts, and the agent_guide flow
// DSL) can gate on it uniformly.
//
// The zero value of Surface is the FULL surface: any field left at its zero
// value is treated as enabled, so a caller that does not opt into a restricted
// surface (and all pre-existing test profiles) keeps full behaviour without
// having to populate every flag.
type Surface struct {
	// Account enables the account, subscription, auth, and API-key operations.
	Account bool
	// Vault enables the Sia durable-storage vault operations, apps, and
	// file tools. Disabled in hosted mode.
	Vault bool
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
	// Admin enables the portal operator/admin operations.
	Admin bool
	// Upload enables the IPFS upload/download custom tool family.
	Upload bool
}

// FullSurface enables every domain and tool family.
var FullSurface = Surface{
	Account:    true,
	Vault:      true,
	Pins:       true,
	Websites:   true,
	DNS:        true,
	IPNS:       true,
	ENS:        true,
	Operations: true,
	Admin:      true,
	Upload:     true,
}

// HostedSurface is the restricted surface for a Portal-embedded ("hosted")
// Pinner MCP server. It exposes account/subscription, IPFS pinning and
// upload, websites, DNS, IPNS, ENS, and operations — but deliberately NOT the
// Sia vault (that remains a CLI/local-MCP concern) and NOT portal admin.
var HostedSurface = Surface{
	Account:    true,
	Pins:       true,
	Websites:   true,
	DNS:        true,
	IPNS:       true,
	ENS:        true,
	Operations: true,
	Upload:     true,
}

// IsZero reports whether s is the zero value (the implicit full surface).
func (s Surface) IsZero() bool {
	return s == (Surface{})
}

// flagOn reports whether the given flag is enabled, treating a zero Surface as
// the full surface. It is the shared gate used by every field accessor, so
// enabling an unset field on an otherwise-zero surface keeps backwards
// compatibility.
func (s Surface) flagOn(set bool) bool {
	if s.IsZero() {
		return true
	}
	return set
}

// AccountOn reports whether the account surface is enabled.
func (s Surface) AccountOn() bool { return s.flagOn(s.Account) }

// VaultOn reports whether the Sia vault surface is enabled.
func (s Surface) VaultOn() bool { return s.flagOn(s.Vault) }

// PinsOn reports whether the IPFS pinning surface is enabled.
func (s Surface) PinsOn() bool { return s.flagOn(s.Pins) }

// WebsitesOn reports whether the website-publishing surface is enabled.
func (s Surface) WebsitesOn() bool { return s.flagOn(s.Websites) }

// DNSOn reports whether the DNS surface is enabled.
func (s Surface) DNSOn() bool { return s.flagOn(s.DNS) }

// IPNSOn reports whether the IPNS surface is enabled.
func (s Surface) IPNSOn() bool { return s.flagOn(s.IPNS) }

// ENSOn reports whether the ENS/onchain surface is enabled.
func (s Surface) ENSOn() bool { return s.flagOn(s.ENS) }

// OperationsOn reports whether the operations-status surface is enabled.
func (s Surface) OperationsOn() bool { return s.flagOn(s.Operations) }

// AdminOn reports whether the portal-admin surface is enabled.
func (s Surface) AdminOn() bool { return s.flagOn(s.Admin) }

// UploadOn reports whether the IPFS upload/download tool family is enabled.
func (s Surface) UploadOn() bool { return s.flagOn(s.Upload) }

// SurfaceIs returns a predicate that matches profiles whose surface has the
// given flag enabled. It lets the platform DSL gate a description, schema,
// branch, or rule on a surface flag using the same predicate machinery as host
// and transport gates. get is one of the surface accessors (e.g. VaultOn).
func SurfaceIs(get func(Surface) bool) Predicate {
	return func(p PlatformProfile) bool { return get(p.Surface) }
}
