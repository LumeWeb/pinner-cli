package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestDNSService_RequireAuthenticated(t *testing.T) {
	t.Run("authenticated with override token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "test-token",
			},
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
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "",
			},
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
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "override-token",
			},
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
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "override-token",
			},
		}

		require.Equal(t, "override-token", svc.getAuthToken())
	})

	t.Run("falls back to config token when override is empty", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "",
			},
		}

		require.Equal(t, "config-token", svc.getAuthToken())
	})

	t.Run("WithDNSAuthToken functional option sets override", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &dnsServiceCLI{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr: cfgMgr,
			},
		}
		WithDNSAuthToken("override-token")(svc)

		require.Equal(t, "override-token", svc.getAuthToken())
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}
