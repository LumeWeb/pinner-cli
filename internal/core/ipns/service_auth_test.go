package ipns

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestIPNSService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "authenticated",
			authToken: "test-token",
			wantErr:   false,
		},
		{
			name:        "not authenticated",
			authToken:   "",
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken: "",
			}).Maybe()

			svc := &service{
				Base: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken(tt.authToken)),
			}

			err := svc.RequireAuthenticated()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIPNSService_AuthTokenOverride(t *testing.T) {
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

	t.Run("WithIPNSAuthToken functional option sets override", func(t *testing.T) {
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
