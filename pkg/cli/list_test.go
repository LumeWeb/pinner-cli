package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestList(t *testing.T) {
	tests := []struct {
		name        string
		nameFilter  string
		limit       int
		setupMocks  func(*configmocks.MockManager, *MockPinningService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful list operation",
			nameFilter: "",
			limit:      0,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
					[]Pin{
						{CID: "QmXxx", Name: "test-name", Status: "pinned", Created: "2024-01-01T00:00:00Z", Metadata: map[string]string{}},
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "successful list with name filter",
			nameFilter: "test-name",
			limit:      0,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "test-name", 0, "").Return(
					[]Pin{
						{CID: "QmXxx", Name: "test-name", Status: "pinned", Created: "2024-01-01T00:00:00Z", Metadata: map[string]string{}},
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "successful list with limit",
			nameFilter: "",
			limit:      10,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 10, "").Return(
					[]Pin{
						{CID: "QmXxx", Name: "test-name", Status: "pinned", Created: "2024-01-01T00:00:00Z", Metadata: map[string]string{}},
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "successful list with empty result",
			nameFilter: "",
			limit:      0,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "", 0, "").Return(
					[]Pin{},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "returns error when list fails",
			nameFilter: "",
			limit:      0,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
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
			name:       "successful list with stdin name filter",
			nameFilter: "backup",
			limit:      0,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().List(mock.Anything, "backup", 0, "").Return(
					[]Pin{
						{CID: "QmXxx", Name: "backup-file", Status: "pinned", Created: "2024-01-01T00:00:00Z", Metadata: map[string]string{}},
					},
					nil,
				)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := newTestConfigMgr(t)
			service := NewMockPinningService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand().
				withString(FlagName, tt.nameFilter).
				withInt(FlagLimit, tt.limit)

			pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
				return service
			}

			err := list(context.Background(), cmd, output, cfgMgr, "", false, pinningServiceFactory)

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

func TestNewListCommand(t *testing.T) {
	t.Run("creates list command with correct configuration", func(t *testing.T) {
		cmd := newListCommand()

		assert.Equal(t, "list", cmd.Name)

		flags := cmd.Flags
		assert.Len(t, flags, 4)

		nameFlag, ok := flags[0].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "name", nameFlag.Name)

		limitFlag, ok := flags[1].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "limit", limitFlag.Name)

		statusFlag, ok := flags[2].(*cli.StringFlag)
		require.True(t, ok)
		assert.Equal(t, "status", statusFlag.Name)

		watchFlag, ok := flags[3].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "watch", watchFlag.Name)
	})
}

func TestList_WithStatusFilter(t *testing.T) {
	cfgMgr := newTestConfigMgr(t)
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().List(mock.Anything, "", 10, "pinned").Return(
		[]Pin{
			{CID: "QmXxx", Name: "test", Status: "pinned", Created: "2024-01-01T00:00:00Z"},
		},
		nil,
	)

	cmd := newMockCommand().
		withInt(FlagLimit, 10).
		withString(FlagStatus, "pinned")

	pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
		return service
	}

	err := list(context.Background(), cmd, output, cfgMgr, "", false, pinningServiceFactory)
	require.NoError(t, err)
}

func TestList_RequireAuthFails(t *testing.T) {
	cfgMgr := newTestConfigMgr(t)
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	cmd := newMockCommand()

	pinningServiceFactory := func(cm config.Manager, out Output, _ bool) PinningService {
		return service
	}

	err := list(context.Background(), cmd, output, cfgMgr, "", false, pinningServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}
