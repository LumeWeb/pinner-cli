package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// The admin service interfaces are re-exported from core. The concrete impls
// (quota/billing/website/profiling) and the AdminTokenProvider live in
// internal/core/admin, which is Output-free. pkg/cli keeps Output-taking
// factory wrappers so the admin command handlers retain their call shape.

type QuotaAdminService = admin.QuotaAdminService
type BillingAdminService = admin.BillingAdminService
type WebsiteAdminService = admin.WebsiteAdminService
type ProfilingAdminService = admin.ProfilingAdminService

// QuotaAdminServiceFactory builds a QuotaAdminService with dependencies.
type QuotaAdminServiceFactory func(cfgMgr config.Manager, output Output) QuotaAdminService

// BillingAdminServiceFactory builds a BillingAdminService with dependencies.
type BillingAdminServiceFactory func(cfgMgr config.Manager, output Output) BillingAdminService

// WebsiteAdminServiceFactory builds a WebsiteAdminService with dependencies.
type WebsiteAdminServiceFactory func(cfgMgr config.Manager, output Output) WebsiteAdminService

// ProfilingAdminServiceFactory builds a ProfilingAdminService with dependencies.
type ProfilingAdminServiceFactory func(cfgMgr config.Manager, output Output) ProfilingAdminService

// default*AdminServiceFactory delegate to the Output-free core factories.
func defaultQuotaAdminServiceFactory(cfgMgr config.Manager, output Output) QuotaAdminService {
	return admin.DefaultQuotaAdminServiceFactory(cfgMgr)
}

func defaultBillingAdminServiceFactory(cfgMgr config.Manager, output Output) BillingAdminService {
	return admin.DefaultBillingAdminServiceFactory(cfgMgr)
}

func defaultWebsiteAdminServiceFactory(cfgMgr config.Manager, output Output) WebsiteAdminService {
	return admin.DefaultWebsiteAdminServiceFactory(cfgMgr)
}

func defaultProfilingAdminServiceFactory(cfgMgr config.Manager, output Output) ProfilingAdminService {
	return admin.DefaultProfilingAdminServiceFactory(cfgMgr)
}

// New*AdminService constructors delegate to the Output-free core constructors.
func NewQuotaAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) QuotaAdminService {
	return admin.NewQuotaAdminService(cfgMgr, apiEndpoint)
}

func NewBillingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) BillingAdminService {
	return admin.NewBillingAdminService(cfgMgr, apiEndpoint)
}

func NewWebsiteAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) WebsiteAdminService {
	return admin.NewWebsiteAdminService(cfgMgr, apiEndpoint)
}

func NewProfilingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) ProfilingAdminService {
	return admin.NewProfilingAdminService(cfgMgr, apiEndpoint)
}
