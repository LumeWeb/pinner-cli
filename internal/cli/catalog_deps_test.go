package cli

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"github.com/stretchr/testify/require"
)

// TestProductionCatalogOpsBundleAssemblesLiveSurface verifies that the
// production catalog-ops deps bundle (wired into the MCP server via
// WithCatalogOps) assembles a real operation catalog exposing model-surface
// tools. This pins the last mile of the compiler-backed migration: the
// production dependency graph produces discoverable operations, not an empty
// or erroring surface. Services are lazy getters, so a stub config manager
// suffices for assembly; only execution would exercise real backends.
func TestProductionCatalogOpsBundleAssemblesLiveSurface(t *testing.T) {
	// Assembly is fully lazy: the deps bundle resolves config/services only at
	// execution time, never during AssembleCatalogOps/Compile. Provide a config
	// factory that would panic if Config() were touched eagerly, proving the
	// surface assembles without a live manager.
	cfgMgr := configmocks.NewMockManager(t)

	prev := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	defer func() { configManagerFactory = prev }()

	bundle := buildCatalogOpsDeps()
	require.NotNil(t, bundle, "production deps bundle must construct")

	oc, err := mcpadapter.AssembleCatalogOps(bundle, mcpadapter.FullSurface, false)
	require.NoError(t, err, "production deps must assemble a catalog")
	require.NotNil(t, oc)

	descs, err := catalog.NewMCPCompiler().Compile(oc)
	require.NoError(t, err)
	require.NotEmpty(t, descs, "compiler must yield a non-empty model surface")

	// Report the actual compiled surface for audit, then verify it spans
	// multiple catalogops domains.
	seen := map[string]bool{}
	for _, d := range descs {
		seen[d.Name] = true
		if testing.Verbose() {
			t.Logf("compiled op: %s", d.Name)
		}
	}

	prefixes := map[string]bool{}
	for name := range seen {
		prefixes[domainPrefix(name)] = true
	}
	require.True(t, len(prefixes) >= 5, "expected >=5 catalogops domains in the compiled surface, got %d: %v", len(prefixes), prefixes)
}

// TestProductionCatalogOpsBundleExposesAdminDomain pins that the admin domain
// shows up in the production MCP surface assembled from buildCatalogOpsDeps.
// The platform-domain tools must be discoverable so other admin sections can
// register operations in the same provider.
func TestProductionCatalogOpsBundleExposesAdminDomain(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	prev := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	defer func() { configManagerFactory = prev }()

	bundle := buildCatalogOpsDeps()
	oc, err := mcpadapter.AssembleCatalogOps(bundle, mcpadapter.FullSurface, false)
	require.NoError(t, err, "production deps must assemble a catalog")

	descs, err := catalog.NewMCPCompiler().Compile(oc)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, d := range descs {
		names[d.Name] = true
	}
	for _, want := range []string{
		"admin_platform_domains_list",
		"admin_platform_domains_register",
		"admin_platform_domains_update",
		"admin_platform_domains_delete",
		"admin_platform_domains_bind",
	} {
		if !names[want] {
			t.Fatalf("production surface missing admin tool %q", want)
		}
	}
}

// domainPrefix returns the leading domain token of a dotted operation name.
func domainPrefix(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[:i]
		}
	}
	return name
}

// TestBuildCatalogOpsDepsForHostedDoesNotMutateGlobalFactory pins that the
// hosted assembly threads its config manager through the wiring instead of
// mutating the package-global configManagerFactory. A prior implementation
// swapped that global to the hosted closure and never restored it, silently
// redirecting every CLI command path (pin/upload/download/config/auth) onto
// the hosted temp-dir config in any process that also hosted the MCP server.
func TestBuildCatalogOpsDepsForHostedDoesNotMutateGlobalFactory(t *testing.T) {
	globalCfg := configmocks.NewMockManager(t)
	hostedCfg := configmocks.NewMockManager(t)

	// Seed the package-global factory with a sentinel that proves it is never
	// overwritten by the hosted assembly.
	prev := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return globalCfg, nil }
	defer func() { configManagerFactory = prev }()

	bundle := BuildCatalogOpsDepsForHosted(hostedCfg)
	require.NotNil(t, bundle, "hosted bundle must construct")

	// The global factory is untouched: it still resolves the sentinel manager.
	resolved, err := configManagerFactory()
	require.NoError(t, err)
	require.Same(t, globalCfg, resolved, "hosted assembly must not swap the package-global configManagerFactory")

	// The hosted bundle resolves the supplied hosted manager, not the global.
	require.Same(t, hostedCfg, bundle.CfgMgr(), "hosted bundle must resolve the supplied hosted config manager")
}
