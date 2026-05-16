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

func TestMetadata(t *testing.T) {
	tests := []struct {
		name             string
		cid              string
		setFlags         []string
		clearFlag        bool
		isSet            map[string]bool
		setupMocks       func(*configmocks.MockManager, *MockPinningService)
		wantErr          bool
		errContains      string
		cfgMgrFactoryErr bool
	}{
		{
			name:      "successful metadata update",
			cid:       "QmXxx",
			setFlags:  []string{"key", "value"},
			clearFlag: false,
			isSet:     map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{"key", "value"}, false).Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "successful metadata update with multiple pairs",
			cid:       "QmXxx",
			setFlags:  []string{"key1", "value1", "key2", "value2"},
			clearFlag: false,
			isSet:     map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{"key1", "value1", "key2", "value2"}, false).Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "successful metadata clear",
			cid:       "QmXxx",
			setFlags:  []string{},
			clearFlag: true,
			isSet:     map[string]bool{FlagClear: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{}, true).Return(nil)
			},
			wantErr: false,
		},
		{
			name:             "returns error when config manager factory fails",
			cid:              "QmXxx",
			setFlags:         []string{"key", "value"},
			clearFlag:        false,
			isSet:            map[string]bool{FlagSet: true},
			setupMocks:       func(cfgMgr *configmocks.MockManager, service *MockPinningService) {},
			wantErr:          true,
			errContains:      "config error",
			cfgMgrFactoryErr: true,
		},
		{
			name:      "returns error when metadata update fails",
			cid:       "QmXxx",
			setFlags:  []string{"key", "value"},
			clearFlag: false,
			isSet:      map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{"key", "value"}, false).Return(
					errors.New("metadata update failed"),
				)
			},
			wantErr:     true,
			errContains: "metadata update failed",
		},
		{
			name:      "returns error for invalid CID",
			cid:       "invalid-cid",
			setFlags:  []string{"key", "value"},
			clearFlag: false,
			isSet:      map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "invalid-cid", []string{"key", "value"}, false).Return(
					errors.New("invalid CID: invalid cid"),
				)
			},
			wantErr:     true,
			errContains: "invalid CID",
		},
		{
			name:      "returns error when pin not found",
			cid:       "QmXxx",
			setFlags:  []string{"key", "value"},
			clearFlag: false,
			isSet:      map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{"key", "value"}, false).Return(
					errors.New("pin not found: QmXxx"),
				)
			},
			wantErr:     true,
			errContains: "pin not found",
		},
		{
			name:      "returns error for invalid metadata pairs",
			cid:       "QmXxx",
			setFlags:  []string{"key"},
			clearFlag: false,
			isSet:      map[string]bool{FlagSet: true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPinningService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdateMetadata(context.Background(), "QmXxx", []string{"key"}, false).Return(
					errors.New("metadata key-value pairs must be provided in pairs"),
				)
			},
			wantErr:     true,
			errContains: "metadata key-value pairs must be provided in pairs",
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

			cmd := &mockMetadataCommand{
				cid:   tt.cid,
				set:   tt.setFlags,
				clear: tt.clearFlag,
				isSet: tt.isSet,
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

			err := metadata(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)

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

func TestNewMetadataCommand(t *testing.T) {
	t.Run("creates metadata command with correct configuration", func(t *testing.T) {
		cmd := newMetadataCommand()

		assert.Equal(t, "metadata", cmd.Name)
		assert.Equal(t, "<cid>", cmd.ArgsUsage)

		// Check flags
		flags := cmd.Flags
		assert.Len(t, flags, 3)

		setFlag, ok := flags[0].(*cli.StringSliceFlag)
		require.True(t, ok)
		assert.Equal(t, "set", setFlag.Name)

		clearFlag, ok := flags[1].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "clear", clearFlag.Name)
	})
}

// mockMetadataCommand is a mock implementation of metadataCommandGetter for testing.
type mockMetadataCommand struct {
	cid    string
	set    []string
	clear  bool
	isSet  map[string]bool
}

func (m *mockMetadataCommand) GetCID() string {
	return m.cid
}

func (m *mockMetadataCommand) StringSlice(name string) []string {
	switch name {
	case FlagSet:
		return m.set
	default:
		return nil
	}
}

func (m *mockMetadataCommand) Bool(name string) bool {
	switch name {
	case FlagClear:
		return m.clear
	default:
		return false
	}
}

func (m *mockMetadataCommand) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
}
