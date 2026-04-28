package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

// mockQuotaPlansCreateCmd implements quotaPlansCreateCmdGetter
type mockQuotaPlansCreateCmd struct {
	name        string
	description string
	upload      int
	download    int
	storage     int
	isActive    bool
	isDefault   bool
}

func (m *mockQuotaPlansCreateCmd) String(s string) string {
	switch s {
	case FlagName:
		return m.name
	case FlagDescription:
		return m.description
	default:
		return ""
	}
}

func (m *mockQuotaPlansCreateCmd) Int(s string) int {
	switch s {
	case FlagUploadLimit:
		return m.upload
	case FlagDownloadLimit:
		return m.download
	case FlagStorageLimit:
		return m.storage
	default:
		return 0
	}
}

func (m *mockQuotaPlansCreateCmd) Bool(s string) bool {
	switch s {
	case FlagIsActive:
		return m.isActive
	case FlagIsDefault:
		return m.isDefault
	default:
		return false
	}
}

// mockQuotaPlansUpdateCmd implements quotaPlansUpdateCmdGetter
type mockQuotaPlansUpdateCmd struct {
	args         *mockArgs
	name         string
	description  string
	upload       int
	download     int
	storage      int
	isActive     bool
	isDefault    bool
	isSetActive  bool
	isSetDefault bool
}

func (m *mockQuotaPlansUpdateCmd) Args() cli.Args {
	return m.args
}

func (m *mockQuotaPlansUpdateCmd) String(s string) string {
	switch s {
	case FlagName:
		return m.name
	case FlagDescription:
		return m.description
	default:
		return ""
	}
}

func (m *mockQuotaPlansUpdateCmd) Int(s string) int {
	switch s {
	case FlagUploadLimit:
		return m.upload
	case FlagDownloadLimit:
		return m.download
	case FlagStorageLimit:
		return m.storage
	default:
		return 0
	}
}

func (m *mockQuotaPlansUpdateCmd) Bool(s string) bool {
	switch s {
	case FlagIsActive:
		return m.isActive
	case FlagIsDefault:
		return m.isDefault
	default:
		return false
	}
}

func (m *mockQuotaPlansUpdateCmd) IsSet(s string) bool {
	switch s {
	case FlagIsActive:
		return m.isSetActive
	case FlagIsDefault:
		return m.isSetDefault
	case FlagUploadLimit, FlagDownloadLimit, FlagStorageLimit:
		return true
	default:
		return false
	}
}

func TestQuotaPlansCreate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockQuotaPlansCreateCmd
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with is-active flag",
			cmd: &mockQuotaPlansCreateCmd{
				name:     "Free",
				isActive: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with is-active and is-default flags",
			cmd: &mockQuotaPlansCreateCmd{
				name:      "Free",
				isActive:  true,
				isDefault: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "0").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "is-default fails but plan still created",
			cmd: &mockQuotaPlansCreateCmd{
				name:      "Free",
				isActive:  true,
				isDefault: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "0").Return(fmt.Errorf("%w: plan not found", admin.ErrNotFound))
			},
			wantErr:     true,
			errContains: "failed to set as default",
		},
		{
			name: "success with description",
			cmd: &mockQuotaPlansCreateCmd{
				name:        "Free",
				description: "Free tier plan",
				isActive:    true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name: "not authenticated",
			cmd: &mockQuotaPlansCreateCmd{
				name: "Free",
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "authentication required",
		},
		{
			name: "service error",
			cmd: &mockQuotaPlansCreateCmd{
				name:     "Free",
				isActive: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(nil, errors.New("api error"))
			},
			wantErr:     true,
			errContains: "api error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := NewOutputFormatter(tt.jsonOutput, false, false, false)

			serviceFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}

			err := quotaPlansCreateAction(context.Background(), tt.cmd, output, cfgMgr, serviceFactory)

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

func TestQuotaPlansSetDefault_Enhanced(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success",
			args: []string{"2"},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "plan not found with helpful error",
			args: []string{"3"},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "3").Return(fmt.Errorf("%w: plan not found", admin.ErrNotFound))
			},
			wantErr:     true,
			errContains: "ensure the plan is active",
		},
		{
			name: "other error passes through",
			args: []string{"2"},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(errors.New("server error"))
			},
			wantErr:     true,
			errContains: "server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := NewOutputFormatter(false, false, false, false)

			serviceFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}

			cmd := &mockQuotaPlansGetCmd{args: &mockArgs{args: tt.args}}
			err := quotaPlansSetDefaultAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestQuotaPlansUpdate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockQuotaPlansUpdateCmd
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with is-active flag",
			cmd: &mockQuotaPlansUpdateCmd{
				args:        &mockArgs{args: []string{"2"}},
				isActive:    true,
				isSetActive: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdatePlan(mock.Anything, "2", mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with is-active and is-default flags",
			cmd: &mockQuotaPlansUpdateCmd{
				args:         &mockArgs{args: []string{"2"}},
				isActive:     true,
				isDefault:    true,
				isSetActive:  true,
				isSetDefault: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdatePlan(mock.Anything, "2", mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "missing plan ID",
			cmd: &mockQuotaPlansUpdateCmd{
				args: &mockArgs{args: []string{}},
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name: "is-default fails but update succeeds",
			cmd: &mockQuotaPlansUpdateCmd{
				args:         &mockArgs{args: []string{"2"}},
				isDefault:    true,
				isSetDefault: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdatePlan(mock.Anything, "2", mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(fmt.Errorf("%w: plan not found", admin.ErrNotFound))
			},
			wantErr:     true,
			errContains: "failed to set as default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := NewOutputFormatter(tt.jsonOutput, false, false, false)

			serviceFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}

			err := quotaPlansUpdateAction(context.Background(), tt.cmd, output, cfgMgr, serviceFactory)

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
