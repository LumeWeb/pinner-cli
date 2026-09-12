package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRegisterDevTools_ShimProvisionsModuleSurface pins that the CLI dev-tools
// registration is a shim over the module-owned surface: a registered catalog
// carries exactly the three dev_* introspection tools (read-only, directly
// visible) from the module's Config.DevTools assembly — the CLI defines no dev
// tool of its own.
func TestRegisterDevTools_ShimProvisionsModuleSurface(t *testing.T) {
	catalog := NewToolCatalog()
	require.NoError(t, registerDevTools(catalog))
	require.Equal(t, 3, catalog.Len())

	want := map[string]bool{"dev_host_env": true, "dev_profile": true, "dev_request": true}
	for _, entry := range catalog.Entries() {
		require.Truef(t, want[entry.Name], "unexpected catalog entry %q from the dev shim", entry.Name)
		require.Truef(t, entry.ReadOnly, "dev tool %q must be read-only", entry.Name)
		require.Truef(t, entry.DirectVisible, "dev tool %q must be directly visible", entry.Name)
	}
}

// TestRegisterDevTools_NilCatalogIsNoOp pins the nil-catalog guard.
func TestRegisterDevTools_NilCatalogIsNoOp(t *testing.T) {
	require.NoError(t, registerDevTools(nil))
}
