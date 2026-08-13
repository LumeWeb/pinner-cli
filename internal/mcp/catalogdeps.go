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
// The bundle deliberately spans the whole catalogops surface: auth, vault,
// vault-setup, pins, websites, dns, ipns, api-keys, and account operations. A
// domain whose deps are nil degrades to ops that fail with a clear
// "service unavailable" error rather than panicking, so the bundle can be added
// incrementally.
type CatalogDepsBundle struct {
	// CfgMgr returns a live config manager for the current invocation.
	CfgMgr func() config.Manager

	Auth      catalogops.AuthDeps
	Vault     catalogops.VaultDeps
	VaultSetup catalogops.VaultDeps
	Pins      catalogops.PinsDeps
	Websites  catalogops.WebsitesDeps
	DNS       catalogops.DNSDeps
	IPNS      catalogops.IPNSDeps
	APIKeys   catalogops.APIKeysDeps
	Operations catalogops.OperationsDeps
}
