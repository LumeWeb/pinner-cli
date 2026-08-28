package services

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

const testTunnelID = "tunnel_0123456789abcdef0123456789abcdef"

// newCmdWithTunnelID builds a minimal cmd with the tunnel flags registered so
// cmd.String("tunnel-id") resolves the --tunnel-id value.
func newCmdWithTunnelID(t *testing.T, tunnelID string) *cli.Command {
	cmd := &cli.Command{Flags: managedServiceFlags()}
	if tunnelID != "" {
		require.NoError(t, cmd.Set("tunnel-id", tunnelID))
	}
	return cmd
}

func newTestConfigMgr(t *testing.T) config.Manager {
	mgr, err := config.NewManager(filepath.Join(t.TempDir(), "config.yaml"))
	require.NoError(t, err)
	return mgr
}

func TestResolveOpenAICredentialsFromTunnelIDFlag(t *testing.T) {
	cmd := newCmdWithTunnelID(t, testTunnelID)
	id, _ := tunnel.ResolveOpenAICredentials(cmd, nil)
	assert.Equal(t, testTunnelID, id)
}

func TestResolveOpenAICredentialsEnvironment(t *testing.T) {
	t.Run("tunnel id from CONTROL_PLANE_TUNNEL_ID", func(t *testing.T) {
		cmd := newCmdWithTunnelID(t, "")
		t.Setenv("CONTROL_PLANE_TUNNEL_ID", testTunnelID)
		id, _ := tunnel.ResolveOpenAICredentials(cmd, nil)
		assert.Equal(t, testTunnelID, id)
	})

	t.Run("tunnel id flag beats env", func(t *testing.T) {
		cmd := newCmdWithTunnelID(t, testTunnelID)
		t.Setenv("CONTROL_PLANE_TUNNEL_ID", "tunnel_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		id, _ := tunnel.ResolveOpenAICredentials(cmd, nil)
		assert.Equal(t, testTunnelID, id)
	})

	t.Run("api key from CONTROL_PLANE_API_KEY", func(t *testing.T) {
		cmd := newCmdWithTunnelID(t, testTunnelID)
		t.Setenv("CONTROL_PLANE_API_KEY", "sk-control")
		_, key := tunnel.ResolveOpenAICredentials(cmd, nil)
		assert.Equal(t, "sk-control", key)
	})

	t.Run("api key falls back to OPENAI_API_KEY", func(t *testing.T) {
		cmd := newCmdWithTunnelID(t, testTunnelID)
		t.Setenv("CONTROL_PLANE_API_KEY", "")
		t.Setenv("OPENAI_API_KEY", "sk-openai")
		_, key := tunnel.ResolveOpenAICredentials(cmd, nil)
		assert.Equal(t, "sk-openai", key)
	})
}

func TestResolveOpenAICredentialsConfigManagerLastResort(t *testing.T) {
	cmd := newCmdWithTunnelID(t, "")
	// Isolate the environment vars that outrank the config-manager last-resort
	// store so a developer's real OPENAI_API_KEY / CONTROL_PLANE_* cannot leak
	// into the assertion.
	t.Setenv("CONTROL_PLANE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CONTROL_PLANE_TUNNEL_ID", "")
	mgr := newTestConfigMgr(t)
	require.NoError(t, mgr.SetTunnelCredential("openai", "tunnel_id", testTunnelID))
	require.NoError(t, mgr.SetTunnelCredential("openai", "api_key", "sk-cfgmgr"))

	id, key := tunnel.ResolveOpenAICredentials(cmd, mgr)
	assert.Equal(t, testTunnelID, id)
	assert.Equal(t, "sk-cfgmgr", key)
}

func TestResolveOpenAICredentialsEmptyWithoutSources(t *testing.T) {
	cmd := newCmdWithTunnelID(t, "")
	t.Setenv("CONTROL_PLANE_TUNNEL_ID", "")
	t.Setenv("CONTROL_PLANE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	id, key := tunnel.ResolveOpenAICredentials(cmd, nil)
	assert.Equal(t, "", id)
	assert.Equal(t, "", key)
}
