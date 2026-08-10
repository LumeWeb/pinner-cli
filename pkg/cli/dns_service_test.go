package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestDNSService_RequireAuthenticated(t *testing.T) {
	t.Run("authenticated with override token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("test-token")),
		}

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})

	t.Run("not authenticated when no token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr),
		}

		err := svc.RequireAuthenticated()
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authenticated")
	})
}

func TestDNSService_AuthTokenOverride(t *testing.T) {
	t.Run("override token takes precedence over empty config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("override-token")),
		}

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})

	t.Run("override token takes precedence over config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("override-token")),
		}

		require.Equal(t, "override-token", svc.GetAuthToken())
	})

	t.Run("falls back to config token when override is empty", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr),
		}

		require.Equal(t, "config-token", svc.GetAuthToken())
	})

	t.Run("WithDNSAuthToken functional option sets override", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsbase.New(cfgMgr),
		}
		WithDNSAuthToken("override-token")(svc)

		require.Equal(t, "override-token", svc.GetAuthToken())
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}
