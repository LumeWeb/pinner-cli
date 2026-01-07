package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestStatus(t *testing.T) {
	tests := []struct {
		name             string
		cid              string
		watchFlag        bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:      "successful status check",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Status(context.Background(), "QmXxx", false).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "successful status check with watch",
			cid:       "QmXxx",
			watchFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Status(context.Background(), "QmXxx", true).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:             "returns error when config manager factory fails",
			cid:              "QmXxx",
			watchFlag:        false,
			setupMocks:       func(cfgMgr *configmocks.MockManager, service *MockPinningService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:      "returns error when status check fails",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Status(context.Background(), "QmXxx", false).Return(
					nil,
					errors.New("status check failed"),
				)
			},
			wantErr:     true,
			errContains: "status check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockPinningService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := &mockStatusCommand{
				cid:   tt.cid,
				watch: tt.watchFlag,
			}

			var cfgMgrFactory ConfigManagerFactory
			if tt.cfgMgrFactoryErr {
				cfgMgrFactory = func() (config.Manager, error) {
					return nil, errors.New("config error")
				}
			} else {
				cfgMgrFactory = func() (config.Manager, error) {
					return cfgMgr, nil
				}
			}

			pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
				return service
			}

			err := status(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewStatusCommand(t *testing.T) {
	t.Run("creates status command with correct configuration", func(t *testing.T) {
		cmd := newStatusCommand()

		assert.Equal(t, "status", cmd.Name)
		assert.Equal(t, "<cid>", cmd.ArgsUsage)

		// Check flags
		flags := cmd.Flags
		assert.Len(t, flags, 1)

		watchFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "watch", watchFlag.Name)
	})
}

// mockStatusCommand is a mock implementation of statusCommandGetter for testing.
type mockStatusCommand struct {
	cid       string
	watch     bool
	stdinCIDs []string
}

func (m *mockStatusCommand) GetCID() string {
	return m.cid
}

func (m *mockStatusCommand) Bool(name string) bool {
	switch name {
	case FlagWatch:
		return m.watch
	default:
		return false
	}
}
