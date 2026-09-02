package cli

import (
	"fmt"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/admin"
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
// resolveConfigFactory returns the effective config-manager factory for a
// wiring call: the explicitly threaded factory when provided, otherwise the
// package-level configManagerFactory var (the on-disk CLI path). Passing a
// factory explicitly lets a hosted assembly scope its override to the catalog
// wiring without mutating the package-global for every CLI command path.
func resolveConfigFactory(factory ...ConfigManagerFactory) ConfigManagerFactory {
	if len(factory) > 0 && factory[0] != nil {
		return factory[0]
	}
	return configManagerFactory
}

func buildCatalogOpsDeps(factory ...ConfigManagerFactory) *mcpadapter.CatalogDepsBundle {
	cfgFactory := resolveConfigFactory(factory...)
	// The vault-setup domain (vault.create / vault.restore) reuses the vault
	// deps and additionally needs the provisioning service. The vault deps are
	// built as a base and the provisioner added so the hand-off operations
	// drive create/restore out-of-band.
	vaultSetupDeps := vaultCatalogDeps(cfgFactory)
	vaultSetupDeps.Provisioner = func() *vault.Provisioner {
		return vault.NewProvisioner()
	}

	return &mcpadapter.CatalogDepsBundle{
		CfgMgr: func() config.Manager {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Auth: catalogops.AuthDeps{
			// Live config manager: resolved per invocation, never frozen.
			CfgMgr: func() config.Manager {
				cfgMgr, err := cfgFactory()
				if err != nil {
					return nil
				}
				return cfgMgr
			},
			// Build an auth service per invocation from the live config's
			// account endpoint so a config edit is picked up at request time.
			// Honor the per-invocation --auth-token override (threaded via the
			// input map by a hosted server's per-request CredentialResolver)
			// so auth_status reflects the calling principal rather than the
			// config-stored credential.
			AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
				endpoint := cfgMgr.Config().GetAccountEndpointSecure()
				if token != "" {
					return defaultAuthServiceFactoryWithToken(cfgMgr, endpoint, token)
				}
				return defaultAuthServiceFactory(cfgMgr, endpoint)
			},
			// Resolve the live auth token from config at request time.
			ResolveAuthToken: func(cfgMgr config.Manager) string {
				return cfgMgr.Config().AuthToken
			},
		},
		Vault:      catalogops.VaultDeps(vaultCatalogDeps(cfgFactory)),
		VaultSetup: catalogops.VaultDeps(vaultSetupDeps),
		Pins:       catalogops.PinsDeps(catalogPinningDeps(cfgFactory)),
		Websites:   catalogops.WebsitesDeps(catalogWebsitesDeps(cfgFactory)),
		DNS:        catalogops.DNSDeps(catalogDNSDeps(cfgFactory)),
		IPNS:       catalogops.IPNSDeps(catalogIPNSDeps(cfgFactory)),
		ENS:        catalogops.ENSDeps(catalogENSDeps(cfgFactory)),
		APIKeys:    catalogops.APIKeysDeps(catalogAPIKeysDeps(cfgFactory)),
		Operations: catalogops.OperationsDeps(catalogOperationsDeps(cfgFactory)),
		Account: catalogops.AccountDeps{
			// Live config manager: resolved per invocation, never frozen.
			CfgMgr: func() config.Manager {
				cfgMgr, err := cfgFactory()
				if err != nil {
					return nil
				}
				return cfgMgr
			},
			// Auth service for the live config's account endpoint, honoring the
			// per-invocation --auth-token override threaded via the input map.
			AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
				endpoint := cfgMgr.Config().GetAccountEndpointSecure()
				if token != "" {
					return defaultAuthServiceFactoryWithToken(cfgMgr, endpoint, token)
				}
				return defaultAuthServiceFactory(cfgMgr, endpoint)
			},
			// Web-app subscription page URL: https://account.<portal>/account/subscription.
			PortalURL: func(cfgMgr config.Manager) string {
				return strings.TrimSuffix(cfgMgr.Config().GetAccountEndpointSecure(), "/") + "/account/subscription"
			},
		},
		Admin: catalogops.AdminDeps{
			// Live config manager shared with the rest of the bundle.
			CfgMgr: func() config.Manager {
				cfgMgr, err := cfgFactory()
				if err != nil {
					return nil
				}
				return cfgMgr
			},
			// Admin services resolve lazily per invocation via the core
			// factories (which perform token exchange on first use). They take
			// the live config manager so an on-disk config/auth-token edit is
			// picked up at request time.
			PlatformDomainAdminService: func(cfgMgr config.Manager) (admin.PlatformDomainAdminService, error) {
				if cfgMgr == nil {
					return nil, fmt.Errorf("no config manager available")
				}
				return admin.DefaultPlatformDomainAdminServiceFactory(cfgMgr), nil
			},
			QuotaAdminService: func(cfgMgr config.Manager) (admin.QuotaAdminService, error) {
				if cfgMgr == nil {
					return nil, fmt.Errorf("no config manager available")
				}
				return admin.DefaultQuotaAdminServiceFactory(cfgMgr), nil
			},
			BillingAdminService: func(cfgMgr config.Manager) (admin.BillingAdminService, error) {
				if cfgMgr == nil {
					return nil, fmt.Errorf("no config manager available")
				}
				return admin.DefaultBillingAdminServiceFactory(cfgMgr), nil
			},
			WebsiteAdminService: func(cfgMgr config.Manager) (admin.WebsiteAdminService, error) {
				if cfgMgr == nil {
					return nil, fmt.Errorf("no config manager available")
				}
				return admin.DefaultWebsiteAdminServiceFactory(cfgMgr), nil
			},
			SocialProviderAdminService: func(cfgMgr config.Manager) (admin.SocialProviderAdminService, error) {
				if cfgMgr == nil {
					return nil, fmt.Errorf("no config manager available")
				}
				return admin.DefaultSocialProviderAdminServiceFactory(cfgMgr), nil
			},
		},
	}
}

// catalogENSDeps builds the catalogops.ENSDeps for the ENS operations. ENS
// reuses the IPNS dependency graph (an ENS pointing is an IPNS key + publish).
func catalogENSDeps(factory ...ConfigManagerFactory) catalogops.ENSDeps {
	return catalogops.ENSDeps{IPNS: catalogIPNSDeps(factory...)}
}
