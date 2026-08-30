package mcp

import (
	"testing"

	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildHostedServerRejectsVaultSync guards the invariant that a hosted
// (Portal-embedded) server never registers the background vault scheduler
// tasks. The Sia vault is surface-disabled in hosted mode and the vault sync/
// upload loops live only in the CLI adapter Action; passing WithVaultSync into
// a hosted assembly must fail loudly rather than silently schedule vault work.
func TestBuildHostedServerRejectsVaultSync(t *testing.T) {
	_, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
		Options: []MCPServerOption{
			WithVaultSync(corevault.SyncLoopConfig{
				Service: func(string) (corevault.VaultService, error) { return nil, nil },
			}),
		},
	})
	require.Error(t, err, "hosted assembly with WithVaultSync must be rejected")
	assert.Contains(t, err.Error(), "vault scheduler")
}

// TestBuildHostedServerWithoutVaultSyncAssembles verifies the default hosted
// assembly (no vault sync and no vault surface) builds cleanly.
func TestBuildHostedServerWithoutVaultSyncAssembles(t *testing.T) {
	_, cat, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
	})
	require.NoError(t, err, "hosted assembly must build without vault sync")
	require.NotNil(t, cat)
}
