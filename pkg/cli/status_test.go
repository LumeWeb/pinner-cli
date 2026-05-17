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
	portalsdk "go.lumeweb.com/portal-sdk"
)

func TestStatus(t *testing.T) {
	tests := []struct {
		name             string
		cid              string
		watchFlag        bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService, *MockStatusService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:      "successful pin status check",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmXxx", false).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "successful pin status check with watch",
			cid:       "QmXxx",
			watchFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmXxx", true).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "fallback to operation status when pin not found",
			cid:       "QmYyy",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmYyy", false).Return(
					nil,
					&OperationStatusResult{
						CID:                   "QmYyy",
						Status:                "completed",
						StatusDisplayName:     "Completed",
						Operation:             "pin",
						OperationDisplayName:  "Pin",
						Protocol:              "ipfs",
						ProtocolDisplayName:   "IPFS",
						ProgressPercent:       100,
						StartedAt:             "2024-01-01T00:00:00Z",
						Source:                "operation",
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "operation status with error details",
			cid:       "QmZzz",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmZzz", false).Return(
					nil,
					&OperationStatusResult{
						CID:                   "QmZzz",
						Status:                "failed",
						StatusDisplayName:     "Failed",
						Operation:             "pin",
						OperationDisplayName:  "Pin",
						Protocol:              "ipfs",
						ProtocolDisplayName:   "IPFS",
						ProgressPercent:       50,
						StartedAt:             "2024-01-01T00:00:00Z",
						Error:                 "upload failed",
						Source:                "operation",
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
			setupMocks:       func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:      "returns error when status check fails",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmXxx", false).Return(
					nil,
					nil,
					errors.New("status check failed"),
				)
			},
			wantErr:     true,
			errContains: "status check failed",
		},
		{
			name:      "returns pin not found when no operation exists either",
			cid:       "QmMissing",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(context.Background(), "QmMissing", false).Return(
					nil,
					nil,
					ErrPinNotFound,
				)
			},
			wantErr:     true,
			errContains: "pin not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			pinningSvc := NewMockPinningService(t)
			statusSvc := NewMockStatusService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, pinningSvc, statusSvc)
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
				return pinningSvc
			}

			statusServiceFactory := func(cm config.Manager, out Output, ps PinningService, acc portalsdk.AccountAPI) StatusService {
				return statusSvc
			}

			err := status(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory, statusServiceFactory)

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

		flags := cmd.Flags
		assert.Len(t, flags, 1)

		watchFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "watch", watchFlag.Name)
	})
}

type mockStatusCommand struct {
	cid   string
	watch bool
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
