package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// TestProvidersMigratedNoLegacyConfigurer guards that every tunnel provider
// install flow now runs through the shared field-resolution primitive
// (Fields + Finalize) and none remains on the legacy imperative Configurer.
// The legacy path still exists on TunnelProviderSpec for future providers, but
// the three current providers (OpenAI, cloudflared, ngrok) must all be migrated.
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
		require.Nil(t, spec.Configurer, "provider %q must NOT use the legacy Configurer", p)
	}
}
