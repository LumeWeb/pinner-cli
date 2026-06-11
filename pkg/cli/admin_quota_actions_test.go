package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func TestQuotaAllowancesCreate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with all flags",
			cmd: newMockCommand().
				withInt(FlagUserID, 42).
				withIsSet(FlagUserID, true).
				withString(FlagSource, "admin").
				withString(FlagQuotaType, "monthly").
				withInt(FlagUploadLimit, 1048576),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreateAllowance(
					mock.Anything, 42, "admin", "monthly", 1048576, 1048576, 0, time.Time{},
				).Return(&admin.QuotaAllowance{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with expiry flag",
			cmd: newMockCommand().
				withInt(FlagUserID, 10).
				withIsSet(FlagUserID, true).
				withString(FlagSource, "grant").
				withString(FlagQuotaType, "one-time").
				withInt(FlagUploadLimit, 2048).
				withInt(FlagExpiry, 30).
				withIsSet(FlagExpiry, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreateAllowance(
					mock.Anything, 10, "grant", "one-time", 2048, 2048, 0, mock.AnythingOfType("time.Time"),
				).Return(&admin.QuotaAllowance{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success json output",
			cmd: newMockCommand().
				withInt(FlagUserID, 5).
				withIsSet(FlagUserID, true).
				withString(FlagSource, "promo").
				withString(FlagQuotaType, "annual").
				withInt(FlagUploadLimit, 512),
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreateAllowance(
					mock.Anything, 5, "promo", "annual", 512, 512, 0, time.Time{},
				).Return(&admin.QuotaAllowance{}, nil)
			},
			wantErr: false,
		},
		{
			name:        "missing user-id flag",
			cmd:         newMockCommand(),
			jsonOutput:  false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "--user-id is required",
		},
		{
			name: "not authenticated",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "authentication required",
		},
		{
			name: "service error",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true).
				withString(FlagSource, "admin").
				withString(FlagQuotaType, "monthly").
				withInt(FlagUploadLimit, 1024),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreateAllowance(
					mock.Anything, 1, "admin", "monthly", 1024, 1024, 0, time.Time{},
				).Return(nil, errors.New("api error"))
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

			savedFactory := quotaAdminServiceFactory
			quotaAdminServiceFactory = func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}
			defer func() { quotaAdminServiceFactory = savedFactory }()

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaAllowancesCreateAction(context.Background(), tt.cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaAllowancesUpdate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with upload-limit",
			cmd: newMockCommand().
				withArgs("grant-123").
				withInt(FlagUploadLimit, 2048).
				withIsSet(FlagUploadLimit, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateAllowance(
					mock.Anything, "grant-123", 0, "", "", 2048, 2048, 0, time.Time{},
				).Return(&admin.QuotaAllowance{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with multiple fields",
			cmd: newMockCommand().
				withArgs("grant-456").
				withInt(FlagUserID, 7).
				withIsSet(FlagUserID, true).
				withString(FlagSource, "admin").
				withIsSet(FlagSource, true).
				withString(FlagQuotaType, "monthly").
				withIsSet(FlagQuotaType, true).
				withInt(FlagUploadLimit, 4096).
				withIsSet(FlagUploadLimit, true).
				withInt(FlagDownloadLimit, 8192).
				withIsSet(FlagDownloadLimit, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateAllowance(
					mock.Anything, "grant-456", 7, "admin", "monthly", 4096, 8192, 0, time.Time{},
				).Return(&admin.QuotaAllowance{}, nil)
			},
			wantErr: false,
		},
		{
			name:        "missing grant ID",
			cmd:         newMockCommand(),
			jsonOutput:  false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "grant ID is required",
		},
		{
			name: "no update fields provided",
			cmd: newMockCommand().
				withArgs("grant-789"),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "at least one field must be provided for update",
		},
		{
			name: "not authenticated",
			cmd: newMockCommand().
				withArgs("grant-1").
				withInt(FlagUploadLimit, 1024).
				withIsSet(FlagUploadLimit, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "service error",
			cmd: newMockCommand().
				withArgs("grant-1").
				withInt(FlagUploadLimit, 1024).
				withIsSet(FlagUploadLimit, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateAllowance(
					mock.Anything, "grant-1", 0, "", "", 1024, 1024, 0, time.Time{},
				).Return(nil, errors.New("allowance not found"))
			},
			wantErr:     true,
			errContains: "allowance not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			savedFactory := quotaAdminServiceFactory
			quotaAdminServiceFactory = func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}
			defer func() { quotaAdminServiceFactory = savedFactory }()

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaAllowancesUpdateAction(context.Background(), tt.cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaReconcile(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success without user-id",
			cmd:        newMockCommand(),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Reconcile(mock.Anything, (*int)(nil)).Return("all quotas reconciled", 5, nil)
			},
			wantErr: false,
		},
		{
			name: "success with user-id",
			cmd: newMockCommand().
				withInt(FlagUserID, 42).
				withIsSet(FlagUserID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Reconcile(mock.Anything, mock.AnythingOfType("*int")).Return("user quotas reconciled", 2, nil)
			},
			wantErr: false,
		},
		{
			name:       "success json output",
			cmd:        newMockCommand(),
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Reconcile(mock.Anything, (*int)(nil)).Return("reconciled", 3, nil)
			},
			wantErr: false,
		},
		{
			name:       "not authenticated",
			cmd:        newMockCommand(),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "authentication required",
		},
		{
			name:       "service error",
			cmd:        newMockCommand(),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Reconcile(mock.Anything, (*int)(nil)).Return("", 0, errors.New("reconciliation failed"))
			},
			wantErr:     true,
			errContains: "reconciliation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			savedFactory := quotaAdminServiceFactory
			quotaAdminServiceFactory = func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}
			defer func() { quotaAdminServiceFactory = savedFactory }()

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaReconcileAction(context.Background(), tt.cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaUserConfigsUpdate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "success with plan-id",
			cmd: newMockCommand().
				withInt(FlagUserID, 10).
				withIsSet(FlagUserID, true).
				withInt(FlagPlanID, 3).
				withIsSet(FlagPlanID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateUserConfig(
					mock.Anything, 10, mock.Anything,
				).Return(&admin.UserQuotaConfig{}, nil)
			},
			wantErr: false,
		},
		{
			name: "success with enforcement-policy",
			cmd: newMockCommand().
				withInt(FlagUserID, 5).
				withIsSet(FlagUserID, true).
				withString(FlagEnforcementPolicy, "HARD_LIMITS").
				withIsSet(FlagEnforcementPolicy, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateUserConfig(
					mock.Anything, 5, mock.Anything,
				).Return(&admin.UserQuotaConfig{}, nil)
			},
			wantErr: false,
		},
		{
			name:        "missing user-id",
			cmd:         newMockCommand(),
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "--user-id is required",
		},
		{
			name: "no update fields provided",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
			},
			wantErr:     true,
			errContains: "at least one field must be provided for update",
		},
		{
			name: "invalid enforcement-policy value",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true).
				withString(FlagEnforcementPolicy, "INVALID").
				withIsSet(FlagEnforcementPolicy, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "invalid --enforcement-policy value",
		},
		{
			name: "not authenticated",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true).
				withInt(FlagPlanID, 2).
				withIsSet(FlagPlanID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "service error",
			cmd: newMockCommand().
				withInt(FlagUserID, 1).
				withIsSet(FlagUserID, true).
				withInt(FlagPlanID, 2).
				withIsSet(FlagPlanID, true),
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().UpdateUserConfig(
					mock.Anything, 1, mock.Anything,
				).Return(nil, errors.New("update failed"))
			},
			wantErr:     true,
			errContains: "update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockQuotaAdminService(t)

			tt.setupMocks(cfgMgr, service)

			savedFactory := quotaAdminServiceFactory
			quotaAdminServiceFactory = func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}
			defer func() { quotaAdminServiceFactory = savedFactory }()

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaUserConfigsUpdateAction(context.Background(), tt.cmd, output, cfgMgr, quotaAdminServiceFactory)

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
