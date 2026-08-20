package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// TestProvidersMigratedNoLegacyConfigurer guards that every tunnel provider
// install flow runs through the shared field-resolution primitive
// (Fields + Finalize) and none remains on the legacy imperative Configurer.
// The legacy Configurer field has been removed from TunnelProviderSpec, so
// this guards that all providers declare the migrated Fields/Finalize hooks.
func TestProvidersMigratedNoLegacyConfigurer(t *testing.T) {
	for _, p := range []tunnel.TunnelProvider{
		tunnel.TunnelProviderOpenAI,
		tunnel.TunnelProviderCloudflared,
		tunnel.TunnelProviderNgrok,
	} {
		spec, ok := providers.spec(p)
		require.True(t, ok, "provider %q must be registered", p)
		require.NotNil(t, spec.Fields, "provider %q must declare Fields (migrated)", p)
		require.NotNil(t, spec.Finalize, "provider %q must declare Finalize (migrated)", p)
	}
}
