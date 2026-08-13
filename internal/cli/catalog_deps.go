package cli

import (
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
)

// buildCatalogOpsDeps assembles the production operation-catalog dependency
// graph (the config manager plus every catalogops domain's live service
// factories) into a fresh *mcpadapter.CatalogDepsBundle. It is handed to the
// MCP server via mcpadapter.WithCatalogOps so the compiler-derived operation
// surface goes live for real invocations, not just unit tests.
//
// Every dependency is a lazy getter/closure resolved per invocation: services
// read cfgMgr.Config() (including the auth token) at request time, mirroring
// the wizard wiring comment about live token reload. A `pinner login` or
// on-disk config edit that relocates the token is therefore picked up by the
// running server without a restart; tokens are never frozen at construction.
//
// The bundle returns a fresh copy each call so a test/global override stays
// live. A domain whose deps are left nil degrades to operations that fail with
// a clear "service unavailable" error at execution time, so the bundle can be
// extended incrementally.
func buildCatalogOpsDeps() *mcpadapter.CatalogDepsBundle {
	// The vault-setup domain (vault.create / vault.restore) reuses the vault
	// deps and additionally needs the provisioning service. The vault deps are
	// built as a base and the provisioner added so the hand-off operations
	// drive create/restore out-of-band.
	vaultSetupDeps := vaultCatalogDeps()
	vaultSetupDeps.Provisioner = func() *vault.Provisioner {
		return vault.NewProvisioner()
	}

	return &mcpadapter.CatalogDepsBundle{
		CfgMgr: func() config.Manager {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Auth: catalogops.AuthDeps{
			// Live config manager: resolved per invocation, never frozen.
			CfgMgr: func() config.Manager {
				cfgMgr, err := configManagerFactory()
				if err != nil {
					return nil
				}
				return cfgMgr
			},
			// Build an auth service per invocation from the live config's
			// account endpoint so a config edit is picked up at request time.
			AuthService: func(cfgMgr config.Manager) auth.AuthService {
				return defaultAuthServiceFactory(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure())
			},
			// Resolve the live auth token from config at request time.
			ResolveAuthToken: func(cfgMgr config.Manager) string {
				return cfgMgr.Config().AuthToken
			},
		},
		Vault:      catalogops.VaultDeps(vaultCatalogDeps()),
		VaultSetup: catalogops.VaultDeps(vaultSetupDeps),
		Pins:       catalogops.PinsDeps(catalogPinningDeps()),
		Websites:   catalogops.WebsitesDeps(catalogWebsitesDeps()),
		DNS:        catalogops.DNSDeps(catalogDNSDeps()),
		IPNS:       catalogops.IPNSDeps(catalogIPNSDeps()),
		APIKeys:    catalogops.APIKeysDeps(catalogAPIKeysDeps()),
		Operations: catalogops.OperationsDeps(catalogOperationsDeps()),
	}
}
