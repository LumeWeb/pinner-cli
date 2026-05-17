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

func TestUnpin(t *testing.T) {
	tests := []struct {
		name             string
		cid              string
		confirmFlag      bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:        "successful unpin operation",
			cid:         "QmXxx",
			confirmFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Unpin(context.Background(), "QmXxx", true).Return(
					&UnpinResult{CID: "QmXxx"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "successful unpin without confirm",
			cid:         "QmXxx",
			confirmFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Unpin(context.Background(), "QmXxx", false).Return(
					&UnpinResult{CID: "QmXxx"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:             "returns error when config manager factory fails",
			cid:              "QmXxx",
			confirmFlag:      true,
			setupMocks:       func(cfgMgr *configmocks.MockManager, service *MockPinningService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:        "returns error when unpin fails",
			cid:         "QmXxx",
			confirmFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Unpin(context.Background(), "QmXxx", true).Return(
					nil, errors.New("unpin failed"),
				)
			},
			wantErr:     true,
			errContains: "unpin failed",
		},
		{
			name:        "returns error for invalid CID",
			cid:         "invalid-cid",
			confirmFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Unpin(context.Background(), "invalid-cid", true).Return(
					nil, errors.New("invalid CID: invalid cid"),
				)
			},
			wantErr:     true,
			errContains: "invalid CID",
		},
		{
			name:        "returns error when pin not found",
			cid:         "QmXxx",
			confirmFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Unpin(context.Background(), "QmXxx", true).Return(
					nil, errors.New("pin not found: QmXxx"),
				)
			},
			wantErr:     true,
			errContains: "pin not found",
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

			cmd := &mockUnpinCommand{
				cid:     tt.cid,
				confirm: tt.confirmFlag,
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

			err := unpin(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestUnpinBatch(t *testing.T) {
	tests := []struct {
		name        string
		cids        string
		confirm     bool
		parallel    int
		continueOn  bool
		setupMocks  func(*configmocks.MockManager, *MockPinningService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful batch unpin operation",
			cids:     "QmXxx1 QmXxx2 QmXxx3",
			confirm:  true,
			parallel: 3,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UnpinBatch(context.Background(), []string{"QmXxx1", "QmXxx2", "QmXxx3"}, BatchOptions{
					Parallel:   3,
					ContinueOn: false,
				}).Return(&BatchResult{
					Total:     3,
					Succeeded: []OperationResult{{CID: "QmXxx1"}, {CID: "QmXxx2"}, {CID: "QmXxx3"}},
					Failed:    []OperationError{},
					Skipped:   []string{},
					Duration:  0,
				}, nil)
			},
			wantErr: false,
		},
		{
			name:     "returns error when no CIDs provided",
			cids:     "",
			confirm:  true,
			parallel: 1,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "CID is required",
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

			cmd := &mockUnpinCommand{
				cid:        tt.cids,
				confirm:    tt.confirm,
				parallel:   tt.parallel,
				continueOn: tt.continueOn,
			}

			cfgMgrFactory := func() (config.Manager, error) {
				return cfgMgr, nil
			}

			pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
				return service
			}

			err := unpin(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestNewUnpinCommand(t *testing.T) {
	t.Run("creates unpin command with correct configuration", func(t *testing.T) {
		cmd := newUnpinCommand()

		assert.Equal(t, "unpin", cmd.Name)
		assert.Equal(t, "<cid...>", cmd.ArgsUsage)

		flags := cmd.Flags
		assert.Len(t, flags, 5)

		confirmFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "confirm", confirmFlag.Name)

		fileFlag, ok := flags[1].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "file", fileFlag.Name)

		parallelFlag, ok := flags[2].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "parallel", parallelFlag.Name)

		continueFlag, ok := flags[3].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "continue", continueFlag.Name)

		assert.Len(t, cmd.Commands, 1)
		assert.Equal(t, "all", cmd.Commands[0].Name)
	})
}

// mockUnpinCommand is a mock implementation of unpinCommandGetter for testing.
type mockUnpinCommand struct {
	cid        string
	confirm    bool
	file       string
	parallel   int
	continueOn bool
}

func (m *mockUnpinCommand) GetCID() string {
	return m.cid
}

func (m *mockUnpinCommand) String(name string) string {
	switch name {
	case FlagFile:
		return m.file
	default:
		return ""
	}
}

func (m *mockUnpinCommand) Int(name string) int {
	switch name {
	case FlagParallel:
		return m.parallel
	default:
		return 0
	}
}

func (m *mockUnpinCommand) Bool(name string) bool {
	switch name {
	case FlagConfirm:
		return m.confirm
	case FlagContinue:
		return m.continueOn
	default:
		return false
	}
}
