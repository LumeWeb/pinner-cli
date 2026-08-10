package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestUnpinAll(t *testing.T) {
	tests := []struct {
		name         string
		confirm      bool
		yes          bool
		dryRun       bool
		statusFilter string
		parallel     int
		continueOn   bool
		setupMocks   func(*configmocks.MockManager, *MockPinningService)
		wantErr      bool
		errContains  string
	}{
		{
			name:    "requires --confirm flag",
			confirm: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
			},
			wantErr: false,
		},
		{
			name:         "successful unpin all with --yes",
			confirm:      true,
			yes:          true,
			statusFilter: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
						{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(mock.Anything, "", BatchOptions{
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
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "failed").Return(
					[]Pin{
						{CID: "QmFailed1", Name: "failed1", Status: "failed", RequestID: "req-f1"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(mock.Anything, "failed", BatchOptions{
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
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
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
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
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
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
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
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(mock.Anything, "", BatchOptions{
					Parallel:   0,
					ContinueOn: false,
					Progress:   true,
				}).Return(nil, errors.New("unpin-all failed"))
			},
			wantErr:     true,
			errContains: "unpin-all failed",
		},
		{
			name:    "returns error when not authenticated",
			confirm: true,
			yes:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
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
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
				}).Maybe()
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
						{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
						{CID: "QmXxx3", Name: "test3", Status: "pinned", RequestID: "req-3"},
					},
					nil,
				)
				service.EXPECT().UnpinAll(mock.Anything, "", BatchOptions{
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
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand().
				withBool(FlagForce, tt.confirm || tt.yes).
				withBool(FlagConfirm, tt.confirm).
				withBool(FlagYes, tt.yes).
				withBool(FlagDryRun, tt.dryRun).
				withString(FlagStatus, tt.statusFilter).
				withInt(FlagParallel, tt.parallel).
				withBool(FlagContinue, tt.continueOn)

			pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
				return service
			}

			prompter := &MockConfirmPrompter{}
			err := unpinAll(context.Background(), cmd, output, cfgMgr, "", true, PinningServiceFactory(pinningServiceFactory), prompter)

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

func TestUnpinAllConfirmPrompt(t *testing.T) {
	t.Run("mismatch_aborts", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		service := NewMockPinningService(t)
		output := newTestOutput()

		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().List(mock.Anything, "", 0, "").Return(
			[]Pin{
				{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
				{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
			},
			nil,
		)

		cmd := newMockCommand().
			withBool(FlagForce, false).
			withBool(FlagConfirm, true).
			withBool(FlagYes, false).
			withBool(FlagDryRun, false).
			withString(FlagStatus, "").
			withInt(FlagParallel, 0).
			withBool(FlagContinue, false)

		pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
			return service
		}

		prompter := &MockConfirmPrompter{ConfirmResult: "wrong"}
		err := unpinAll(context.Background(), cmd, output, cfgMgr, "", true, PinningServiceFactory(pinningServiceFactory), prompter)

		assert.ErrorIs(t, err, ErrUnpinAllAborted)
	})

	t.Run("match_proceeds", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		service := NewMockPinningService(t)
		output := newTestOutput()

		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().List(mock.Anything, "", 0, "").Return(
			[]Pin{
				{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
				{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
			},
			nil,
		)
		service.EXPECT().UnpinAll(mock.Anything, "", BatchOptions{
			Parallel:   0,
			ContinueOn: false,
			Progress:   true,
		}).Return(&BatchResult{
			Total:     2,
			Succeeded: []OperationResult{{CID: "QmXxx1"}, {CID: "QmXxx2"}},
			Failed:    []OperationError{},
			Skipped:   []string{},
		}, nil)

		cmd := newMockCommand().
			withBool(FlagForce, false).
			withBool(FlagConfirm, true).
			withBool(FlagYes, false).
			withBool(FlagDryRun, false).
			withString(FlagStatus, "").
			withInt(FlagParallel, 0).
			withBool(FlagContinue, false)

		pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
			return service
		}

		prompter := &MockConfirmPrompter{ConfirmResult: "2"}
		err := unpinAll(context.Background(), cmd, output, cfgMgr, "", true, PinningServiceFactory(pinningServiceFactory), prompter)

		assert.NoError(t, err)
	})

	t.Run("interrupt_aborts", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		service := NewMockPinningService(t)
		output := newTestOutput()

		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().List(mock.Anything, "", 0, "").Return(
			[]Pin{
				{CID: "QmXxx1", Name: "test1", Status: "pinned", RequestID: "req-1"},
				{CID: "QmXxx2", Name: "test2", Status: "pinned", RequestID: "req-2"},
			},
			nil,
		)

		cmd := newMockCommand().
			withBool(FlagForce, false).
			withBool(FlagConfirm, true).
			withBool(FlagYes, false).
			withBool(FlagDryRun, false).
			withString(FlagStatus, "").
			withInt(FlagParallel, 0).
			withBool(FlagContinue, false)

		pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
			return service
		}

		prompter := &MockConfirmPrompter{ConfirmErr: ErrUnpinAllAborted}
		err := unpinAll(context.Background(), cmd, output, cfgMgr, "", true, PinningServiceFactory(pinningServiceFactory), prompter)

		assert.ErrorIs(t, err, ErrUnpinAllAborted)
	})
}

func TestNewUnpinAllCommand(t *testing.T) {
	t.Run("creates unpin all command with correct configuration", func(t *testing.T) {
		cmd := newUnpinAllCommand()

		assert.Equal(t, "all", cmd.Name)

		flags := cmd.Flags
		assert.Len(t, flags, 7)

		forceFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "force", forceFlag.Name)

		confirmFlag, ok := flags[1].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "confirm", confirmFlag.Name)
		assert.True(t, confirmFlag.Hidden)

		statusFlag, ok := flags[2].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "status", statusFlag.Name)

		parallelFlag, ok := flags[3].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "parallel", parallelFlag.Name)

		continueFlag, ok := flags[4].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "continue", continueFlag.Name)

		dryRunFlag, ok := flags[5].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "dry-run", dryRunFlag.Name)

		yesFlag, ok := flags[6].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "yes", yesFlag.Name)
		// --yes is exposed (not hidden) so non-interactive/agent callers can
		// accept the destructive safety prompt; it still requires --force.
		assert.False(t, yesFlag.Hidden)
	})
}
