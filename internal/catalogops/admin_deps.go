// Package catalogops implements the admin domain operations for the operation
// catalog. Each operation drives a core admin service directly and returns
// typed data; rendering happens in the frontend wiring layer.
//
// This file defines AdminDeps, the shared dependency graph for admin
// operations, and AdminOperations, which returns every admin operation for the
// catalog to register.
package catalogops

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// AdminDeps are the dependencies the admin operations need at construction
// time. All getters are resolved per invocation (never a package-init snapshot)
// so services always use fresh config and the auth token is read at request
// time.
//
// Every service getter takes the live config manager and returns the core
// admin service (which performs lazy token exchange on first use). A nil
// getter makes the corresponding operations degrade to a clear "service
// unavailable" error at execution time, so the bundle can be added to
// incrementally.
type AdminDeps struct {
	// CfgMgr returns a live config manager for the current invocation. When
	// nil, the service getters fail to resolve (ops error out).
	CfgMgr func() config.Manager

	// QuotaAdminService resolves the core admin.QuotaAdminService for the
	// current invocation.
	QuotaAdminService func(cfgMgr config.Manager) (admin.QuotaAdminService, error)
	// BillingAdminService resolves the core admin.BillingAdminService.
	BillingAdminService func(cfgMgr config.Manager) (admin.BillingAdminService, error)
	// WebsiteAdminService resolves the core admin.WebsiteAdminService.
	WebsiteAdminService func(cfgMgr config.Manager) (admin.WebsiteAdminService, error)
	// PlatformDomainAdminService resolves the core admin.PlatformDomainAdminService.
	PlatformDomainAdminService func(cfgMgr config.Manager) (admin.PlatformDomainAdminService, error)
}

// config returns the live config manager for this invocation, or nil.
func (d AdminDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// requireConfig resolves the live config manager, erroring clearly when the
// domain is unwired (no CfgMgr) so an unconfigured AdminDeps never panics.
func (d AdminDeps) requireConfig() (config.Manager, error) {
	cfgMgr := d.config()
	if cfgMgr == nil {
		return nil, fmt.Errorf("catalogops: no config manager available for admin operations")
	}
	return cfgMgr, nil
}

// resolveService resolves a lazy admin service getter, erroring clearly when
// the getter is nil (the admin service is not wired).
func resolveService[T any](cfgMgr config.Manager, getter func(cfgMgr config.Manager) (T, error), what string) (T, error) {
	var zero T
	if getter == nil {
		return zero, fmt.Errorf("catalogops: admin %s service unavailable (not wired)", what)
	}
	if cfgMgr == nil {
		return zero, fmt.Errorf("catalogops: no config manager available for admin %s", what)
	}
	return getter(cfgMgr)
}

// platformDomains resolves the PlatformDomainAdminService for this invocation.
func (d AdminDeps) platformDomains() (admin.PlatformDomainAdminService, error) {
	cfgMgr, err := d.requireConfig()
	if err != nil {
		return nil, err
	}
	return resolveService(cfgMgr, d.PlatformDomainAdminService, "platform-domain")
}

// websites resolves the WebsiteAdminService for this invocation.
func (d AdminDeps) websites() (admin.WebsiteAdminService, error) {
	cfgMgr, err := d.requireConfig()
	if err != nil {
		return nil, err
	}
	return resolveService(cfgMgr, d.WebsiteAdminService, "website")
}

// AdminOperations returns the catalog operations for the admin domain. Each
// admin section registers its operations here.
func AdminOperations(d AdminDeps) []catalog.Operation {
	return []catalog.Operation{
		// admin platform-domains
		adminPlatformDomainsList(d),
		adminPlatformDomainsRegister(d),
		adminPlatformDomainsUpdate(d),
		adminPlatformDomainsDelete(d),
		adminPlatformDomainsBind(d),
		// admin websites
		adminWebsitesBlock(d),
		adminWebsitesUnblock(d),
	}
}
