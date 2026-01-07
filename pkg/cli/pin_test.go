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

func TestPinDryRun(t *testing.T) {
	tests := []struct {
		name        string
		cid         string
		dryRunFlag  bool
		setupMocks  func(*configmocks.MockManager, *MockPinningService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "dry run with single CID",
			cid:        "QmXxx",
			dryRunFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "dry run with multiple CIDs",
			cid:        "QmXxx QmYyy QmZzz",
			dryRunFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "dry run with options",
			cid:        "QmXxx",
			dryRunFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr: false,
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

			var cmd pinCommandGetter
			if tt.name == "dry run with options" {
				cmd = &mockPinCommand{
					cid:        tt.cid,
					name:       "test-name",
					wait:       true,
					parallel:   5,
					continueOn: true,
					dryRun:     tt.dryRunFlag,
				}
			} else {
				cmd = &mockPinCommand{
					cid:    tt.cid,
					dryRun: tt.dryRunFlag,
				}
			}

			cfgMgrFactory := func() (config.Manager, error) {
				return cfgMgr, nil
			}

			pinningServiceFactory := func(cfgMgr config.Manager, output Output) PinningService {
				return service
			}

			err := pin(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestPin(t *testing.T) {
	tests := []struct {
		name             string
		cid              string
		nameFlag         string
		waitFlag         bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:     "successful pin operation",
			cid:      "QmXxx",
			nameFlag: "",
			waitFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Pin(context.Background(), "QmXxx", "", false).Return(
					&PinResult{CID: "QmXxx", RequestID: "req-123", Status: "queued"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "successful pin with name",
			cid:      "QmXxx",
			nameFlag: "test-name",
			waitFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Pin(context.Background(), "QmXxx", "test-name", false).Return(
					&PinResult{CID: "QmXxx", RequestID: "req-123", Status: "queued"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "successful pin with wait",
			cid:      "QmXxx",
			nameFlag: "",
			waitFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Pin(context.Background(), "QmXxx", "", true).Return(
					&PinResult{CID: "QmXxx", RequestID: "req-123", Status: "pinned"}, nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "returns error when CID is missing",
			cid:      "",
			nameFlag: "",
			waitFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:          true,
			errContains:      "cid is required",
			cfgMgrFactoryErr: false,
		},
		{
			name:     "returns error when no CIDs provided for batch",
			cid:      "",
			nameFlag: "",
			waitFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "cid is required",
		},
		{
			name:             "returns error when config manager factory fails",
			cid:              "QmXxx",
			nameFlag:         "",
			waitFlag:         false,
			setupMocks:       func(cfgMgr *configmocks.MockManager, service *MockPinningService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:     "returns error when pinning fails",
			cid:      "QmXxx",
			nameFlag: "",
			waitFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().Pin(context.Background(), "QmXxx", "", false).Return(
					nil, errors.New("pinning failed"),
				)
			},
			wantErr:     true,
			errContains: "pinning failed",
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

			cmd := &mockPinCommand{
				cid:  tt.cid,
				name: tt.nameFlag,
				wait: tt.waitFlag,
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

			err := pin(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestPinBatch(t *testing.T) {
	tests := []struct {
		name        string
		cids        string
		parallel    int
		continueOn  bool
		setupMocks  func(*configmocks.MockManager, *MockPinningService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful batch pin operation",
			cids:     "QmXxx1 QmXxx2 QmXxx3",
			parallel: 2,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().PinBatch(context.Background(), []string{"QmXxx1", "QmXxx2", "QmXxx3"}, "", BatchOptions{
					Parallel:   2,
					ContinueOn: false,
					Wait:       false,
					Progress:   true,
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
			parallel: 1,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "cid is required",
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

			cmd := &mockPinCommand{
				cid:        tt.cids,
				parallel:   tt.parallel,
				continueOn: tt.continueOn,
			}

			cfgMgrFactory := func() (config.Manager, error) {
				return cfgMgr, nil
			}

			pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
				return service
			}

			err := pin(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestNewPinCommand(t *testing.T) {
	t.Run("creates pin command with correct configuration", func(t *testing.T) {
		cmd := newPinCommand()

		assert.Equal(t, "pin", cmd.Name)
		assert.Equal(t, "<cid...>", cmd.ArgsUsage)

		// Check flags
		flags := cmd.Flags
		assert.Len(t, flags, 6)

		nameFlag, ok := flags[0].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "name", nameFlag.Name)

		waitFlag, ok := flags[1].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "wait", waitFlag.Name)

		fileFlag, ok := flags[2].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "file", fileFlag.Name)

		parallelFlag, ok := flags[3].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "parallel", parallelFlag.Name)

		continueFlag, ok := flags[4].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "continue", continueFlag.Name)
	})
}

// mockPinCommand is a mock implementation of commandGetter for testing.
type mockPinCommand struct {
	cid        string
	name       string
	wait       bool
	file       string
	parallel   int
	continueOn bool
	dryRun     bool
}

func (m *mockPinCommand) GetCID() string {
	return m.cid
}

func (m *mockPinCommand) String(name string) string {
	switch name {
	case FlagName:
		return m.name
	case FlagFile:
		return m.file
	default:
		return ""
	}
}

func (m *mockPinCommand) Int(name string) int {
	switch name {
	case FlagParallel:
		return m.parallel
	default:
		return 0
	}
}

func (m *mockPinCommand) Bool(name string) bool {
	switch name {
	case FlagWait:
		return m.wait
	case FlagContinue:
		return m.continueOn
	case FlagDryRun:
		return m.dryRun
	default:
		return false
	}
}

func TestDefaultPinningServiceFactory(t *testing.T) {
	t.Run("creates pinning service with correct dependencies", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			BaseEndpoint: "https://api.test.com",
			AuthToken:    "test-token",
			Secure:       true,
		})

		output := NewOutputFormatter(false, false, false, false)

		service := defaultPinningServiceFactory(cfgMgr, output)

		assert.IsType(t, &PinningServiceDefault{}, service)
		ps := service.(*PinningServiceDefault)
		assert.NotNil(t, ps.pinningClient)
		assert.Equal(t, cfgMgr, ps.configMgr)
		assert.Equal(t, output, ps.output)
		assert.Equal(t, "https://ipfs.api.test.com", ps.apiEndpoint)
	})
}
