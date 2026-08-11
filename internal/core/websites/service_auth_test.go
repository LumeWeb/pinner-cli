package websites

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestService_AuthTokenOverride(t *testing.T) {
	t.Run("override token takes precedence over empty config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &service{
			Base: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("override-token")),
		}

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})

	t.Run("override token takes precedence over config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &service{
			Base: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("override-token")),
		}

		require.Equal(t, "override-token", svc.GetAuthToken())
	})

	t.Run("falls back to config token when override is empty", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &service{
			Base: ipfsbase.New(cfgMgr),
		}

		require.Equal(t, "config-token", svc.GetAuthToken())
	})

	t.Run("WithAuthToken functional option sets override", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &service{
			Base: ipfsbase.New(cfgMgr),
		}
		WithAuthToken("override-token")(svc)

		require.Equal(t, "override-token", svc.GetAuthToken())
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}

// TestNewAuthenticated_RequiresAuth exercises the production NewAuthenticated
// entry point, which is what the CLI handlers use and what enforces the auth
// boundary. Guards against silently dropping RequireAuthenticated from core.
func TestNewAuthenticated_RequiresAuth(t *testing.T) {
	t.Run("no token returns not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc, err := NewAuthenticated(cfgMgr, "", true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authenticated")
		require.Nil(t, svc)
	})

	t.Run("explicit auth token builds authenticated service", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc, err := NewAuthenticated(cfgMgr, "test-token", true)
		require.NoError(t, err)
		require.NotNil(t, svc)
		err = svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}
