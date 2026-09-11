package cli

import (
	"go.lumeweb.com/pinner/core/admin"
	"go.lumeweb.com/pinner/core/config"
)

// The admin service interfaces are re-exported from core. The concrete impls
// (quota/billing/website/profiling) and the AdminTokenProvider live in
// internal/core/admin, which is Output-free. pkg/cli keeps Output-taking
// factory wrappers so the admin command handlers retain their call shape.
//
// Only the profiling service is wired through these Output-taking wrappers
// today (see admin_pprof.go). The quota/billing/website/platform-domain admin
// surfaces are driven directly through the Output-free core factories by the
// catalog wiring (catalog_deps.go, catalog_admin_wiring.go), so their legacy
// cli::*AdminService aliases/factories/constructors have been removed.

type ProfilingAdminService = admin.ProfilingAdminService

// ProfilingAdminServiceFactory builds a ProfilingAdminService with dependencies.
type ProfilingAdminServiceFactory func(cfgMgr config.Manager, output Output) ProfilingAdminService

// defaultProfilingAdminServiceFactory delegates to the Output-free core factory.
func defaultProfilingAdminServiceFactory(cfgMgr config.Manager, output Output) ProfilingAdminService {
	return admin.DefaultProfilingAdminServiceFactory(cfgMgr)
}

// NewProfilingAdminService delegates to the Output-free core constructor.
func NewProfilingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) ProfilingAdminService {
	return admin.NewProfilingAdminService(cfgMgr, apiEndpoint)
}
