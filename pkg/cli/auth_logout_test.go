package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestAuthLogout(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful logout when authenticated",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken: "authenticated",
				}).Maybe()
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().SetAuthToken("").Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "successful logout with JSON output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken: "authenticated",
				}).Maybe()
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().SetAuthToken("").Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "not authenticated - human output",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken: "",
				}).Maybe()
			},
			wantErr: false,
		},
		{
			name:       "not authenticated - JSON output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken: "",
				}).Maybe()
			},
			wantErr: false,
		},
		{
			name:        "config manager factory fails",
			jsonOutput:  false,
			setupMocks:  nil,
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
		{
			name:       "SetAuthToken fails",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken: "authenticated",
				}).Maybe()
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().SetAuthToken("").Return(errors.New("disk write error"))
			},
			wantErr:     true,
			errContains: "failed to clear auth token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			output := NewOutputFormatter(tt.jsonOutput, false, false, false)

			var cfgMgrFactory ConfigManagerFactory
			if tt.setupMocks == nil {
				cfgMgrFactory = func() (config.Manager, error) {
					return nil, errors.New("config error")
				}
			} else {
				tt.setupMocks(cfgMgr)
				cfgMgrFactory = func() (config.Manager, error) {
					return cfgMgr, nil
				}
			}

			err := authLogout(context.Background(), output, cfgMgrFactory)

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

func TestAuthLogoutCommand(t *testing.T) {
	// Verify the command is registered and has correct metadata
	cmd := newAuthLogoutCommand()
	require.Equal(t, "logout", cmd.Name)
	require.Equal(t, "Clear stored authentication token", cmd.Usage)
	require.NotNil(t, cmd.Action)
}
