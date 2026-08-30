package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// CatalogDepsBundle carries the concrete dependency graph the operation-catalog
// MCP surface needs to construct every catalogops domain. It is built by the CLI
// wiring layer (internal/cli) - which has the config manager and all core service
// factories - and handed to the MCP server via WithCatalogOps. Each field is a
// getter/closure resolved per invocation (the lazy-deps pattern used throughout
// catalogops) so a test/global override stays live and services always use fresh
// config, never a package-init snapshot.
//
// The bundle deliberately spans the whole catalogops surface: auth, account,
// vault, vault-setup, pins, websites, dns, ipns, ens, api-keys, operations, and
// admin. A domain whose deps are nil degrades to ops that fail with a clear
// "service unavailable" error rather than panicking, so the bundle can be added
// incrementally.
type CatalogDepsBundle struct {
	// CfgMgr returns a live config manager for the current invocation.
	CfgMgr func() config.Manager

	// CredentialResolver resolves the Portal API token for the authenticated
	// request. When nil, the CLI/local default (read the bearer token from
	// config) is used. A hosted server sets this to map a Portal-authenticated
	// user onto a Portal API JWT.
	CredentialResolver CredentialResolver

	Auth      catalogops.AuthDeps
	Account   catalogops.AccountDeps
	Vault     catalogops.VaultDeps
	VaultSetup catalogops.VaultDeps
	Pins      catalogops.PinsDeps
	Websites  catalogops.WebsitesDeps
	DNS       catalogops.DNSDeps
	IPNS      catalogops.IPNSDeps
	ENS       catalogops.ENSDeps
	APIKeys   catalogops.APIKeysDeps
	Operations catalogops.OperationsDeps
	Admin     catalogops.AdminDeps
}

// buildCatalogOpt configures buildCatalog. It is a functional option so the
// existing positional signature of buildCatalog stays intact and all current
// positional-only callers compile and behave unchanged. A later unit will read
// the configured factory off the returned ToolCatalog to populate the surface.
type buildCatalogOpt func(*buildCatalogConfig) error

// buildCatalogConfig carries the resolved buildCatalog options.
type buildCatalogConfig struct {
	// catalogDeps, when set, supplies the operation-catalog dependency
	// factory to store on the returned ToolCatalog. The factory is lazily
	// resolved per invocation (the catalogops lazy-deps pattern) so a
	// test/global override stays live.
	catalogDeps func() *CatalogDepsBundle
	// surface declares which operation domains/tool families the server
	// exposes. The zero value is the full surface (applied via
	// buildCatalogConfig.resolveSurface).
	surface Surface
}

// withCatalogDeps sets the operation-catalog dependency factory that buildCatalog
// stores on the returned ToolCatalog.
func withCatalogDeps(f func() *CatalogDepsBundle) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.catalogDeps = f
		return nil
	}
}

// withSurface sets the server construction surface. Callers that do not opt
// into a restricted surface leave it unset, which resolves to the full surface.
func withSurface(s Surface) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.surface = s
		return nil
	}
}

// resolveSurface normalizes the configured surface: the zero value is the full
// surface.
func (c *buildCatalogConfig) resolveSurface() Surface {
	if c.surface.IsZero() {
		return FullSurface
	}
	return c.surface
}
