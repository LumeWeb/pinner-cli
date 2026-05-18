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

func TestUnpinAll(t *testing.T) {
	tests := []struct {
		name             string
		confirm          bool
		yes              bool
		dryRun           bool
		statusFilter     string
		parallel         int
		continueOn       bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:    "requires --confirm flag",
			confirm: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
			},
			wantErr: false,
		},
		{
			name:         "successful unpin all with --yes",
			confirm:      true,
			yes:          true,
			statusFilter: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
						{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(context.Background(), "", BatchOptions{
					Parallel:   0,
					ContinueOn: false,
					Progress:   true,
				}).Return(&BatchResult{
					Total:     2,
					Succeeded: []OperationResult{{CID: "QmXxx1"}, {CID: "QmXxx2"}},
					Failed:    []OperationError{},
					Skipped:   []string{},
				}, nil)
			},
			wantErr: false,
		},
		{
			name:         "successful unpin all with status filter",
			confirm:      true,
			yes:          true,
			statusFilter: "failed",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "failed").Return(
					[]Pin{
						{CID: "QmFailed1", Name: "failed1", Status: "failed", RequestID: "req-f1"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(context.Background(), "failed", BatchOptions{
					Parallel:   0,
					ContinueOn: false,
					Progress:   true,
				}).Return(&BatchResult{
					Total:     1,
					Succeeded: []OperationResult{{CID: "QmFailed1"}},
					Failed:    []OperationError{},
					Skipped:   []string{},
				}, nil)
			},
			wantErr: false,
		},
		{
			name:    "no pins found",
			confirm: true,
			yes:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					[]Pin{},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:    "dry run shows preview",
			confirm: true,
			dryRun:  true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:    "returns error when list fails",
			confirm: true,
			yes:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					nil,
					errors.New("list failed"),
				)
			},
			wantErr:     true,
			errContains: "list failed",
		},
		{
			name:    "returns error when unpin-all fails",
			confirm: true,
			yes:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(context.Background(), "", BatchOptions{
					Parallel:   0,
					ContinueOn: false,
					Progress:   true,
				}).Return(nil, errors.New("unpin-all failed"))
			},
			wantErr:     true,
			errContains: "unpin-all failed",
		},
		{
			name:             "returns error when config manager factory fails",
			confirm:          true,
			yes:              true,
			setupMocks:       func(cfgMgr *configmocks.MockManager, service *MockPinningService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:    "returns error when not authenticated",
			confirm: true,
			yes:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:       "successful unpin all with --parallel and --continue",
			confirm:    true,
			yes:        true,
			parallel:   5,
			continueOn: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(context.Background(), "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
						{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
						{CID: "QmXxx3", Name: "test3", Status: "pinned", RequestID: "req-3"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(context.Background(), "", BatchOptions{
					Parallel:   5,
					ContinueOn: true,
					Progress:   true,
				}).Return(&BatchResult{
					Total:     3,
					Succeeded: []OperationResult{{CID: "QmXxx1"}, {CID: "QmXxx2"}, {CID: "QmXxx3"}},
					Failed:    []OperationError{},
					Skipped:   []string{},
				}, nil)
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

			cmd := &mockUnpinAllCommand{
				confirm:      tt.confirm,
				yes:          tt.yes,
				dryRun:       tt.dryRun,
				statusFilter: tt.statusFilter,
				parallel:     tt.parallel,
				continueOn:   tt.continueOn,
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

			err := unpinAll(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestNewUnpinAllCommand(t *testing.T) {
	t.Run("creates unpin all command with correct configuration", func(t *testing.T) {
		cmd := newUnpinAllCommand()

		assert.Equal(t, "all", cmd.Name)

		flags := cmd.Flags
		assert.Len(t, flags, 6)

		confirmFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "confirm", confirmFlag.Name)

		statusFlag, ok := flags[1].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "status", statusFlag.Name)

		parallelFlag, ok := flags[2].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "parallel", parallelFlag.Name)

		continueFlag, ok := flags[3].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "continue", continueFlag.Name)

		dryRunFlag, ok := flags[4].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "dry-run", dryRunFlag.Name)

		yesFlag, ok := flags[5].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "yes", yesFlag.Name)
	})
}

type mockUnpinAllCommand struct {
	confirm      bool
	yes          bool
	dryRun       bool
	statusFilter string
	parallel     int
	continueOn   bool
}

func (m *mockUnpinAllCommand) String(name string) string {
	switch name {
	case FlagStatus:
		return m.statusFilter
	default:
		return ""
	}
}

func (m *mockUnpinAllCommand) Int(name string) int {
	switch name {
	case FlagParallel:
		return m.parallel
	default:
		return 0
	}
}

func (m *mockUnpinAllCommand) Bool(name string) bool {
	switch name {
	case FlagConfirm:
		return m.confirm
	case FlagYes:
		return m.yes
	case FlagDryRun:
		return m.dryRun
	case FlagContinue:
		return m.continueOn
	default:
		return false
	}
}
