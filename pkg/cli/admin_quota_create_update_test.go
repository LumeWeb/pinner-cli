package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func TestQuotaPlansCreate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with is-active flag",
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true).
				withBool(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with is-active and is-default flags",
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true).
				withBool(FlagIsActive, true).
				withBool(FlagIsDefault, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "0").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "is-default fails but plan still created",
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true).
				withBool(FlagIsActive, true).
				withBool(FlagIsDefault, true),
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
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true).
				withString(FlagDescription, "Free tier plan").
				withBool(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name: "not authenticated",
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "authentication required",
		},
		{
			name: "service error",
			cmd: newMockCommand().
				withString(FlagName, "Free").
				withIsSet(FlagName, true).
				withBool(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePlan(mock.Anything, mock.AnythingOfType("*admin.QuotaPlan")).Return(nil, errors.New("api error"))
			},
			wantErr:     true,
			errContains: "api error",
		},
		{
			name: "returns error when no fields provided for create",
			cmd: newMockCommand(),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "at least one field must be provided for update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := newTestOutput()

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

func TestQuotaPlansUpdate(t *testing.T) {
	tests := []struct {
		name        string
		planID      string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "success with is-active flag",
			planID: "2",
			cmd: newMockCommand().
				withBool(FlagIsActive, true).
				withIsSet(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "2").Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().UpdatePlan(mock.Anything, "2", mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name:   "success with is-active and is-default flags",
			planID: "2",
			cmd: newMockCommand().
				withBool(FlagIsActive, true).
				withIsSet(FlagIsActive, true).
				withBool(FlagIsDefault, true).
				withIsSet(FlagIsDefault, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "2").Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().UpdatePlan(mock.Anything, "2", mock.AnythingOfType("*admin.QuotaPlan")).Return(&admin.QuotaPlan{}, nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(nil)
			},
			wantErr: false,
		},
		{
			name:   "missing plan ID",
			planID: "",
			cmd:   newMockCommand(),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:   "is-default fails but update succeeds",
			planID: "2",
			cmd: newMockCommand().
				withBool(FlagIsDefault, true).
				withIsSet(FlagIsDefault, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "2").Return(&admin.QuotaPlan{}, nil)
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

			output := newTestOutput()

			serviceFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}

			cmd := tt.cmd
			if tt.planID != "" {
				cmd = cmd.withArgs(tt.planID)
			}

			err := quotaPlansUpdateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
