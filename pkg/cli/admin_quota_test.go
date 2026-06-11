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

func TestQuotaPlansList(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with plans",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListPlans(mock.Anything).Return([]*admin.QuotaPlan{}, 2, nil)
			},
			wantErr: false,
		},
		{
			name:       "success with empty list",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListPlans(mock.Anything).Return([]*admin.QuotaPlan{}, 0, nil)
			},
			wantErr: false,
		},
		{
			name:       "success json output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListPlans(mock.Anything).Return([]*admin.QuotaPlan{}, 2, nil)
			},
			wantErr: false,
		},
		{
			name:       "not authenticated",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "authentication required",
		},
		{
			name:       "service error",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListPlans(mock.Anything).Return(nil, 0, errors.New("api error"))
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

			err := quotaPlansListAction(context.Background(), output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaPlansGet(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "1").Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name:       "json output",
			args:       []string{"1"},
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "1").Return(&admin.QuotaPlan{}, nil)
			},
			wantErr: false,
		},
		{
			name:        "missing plan ID",
			args:        []string{},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:       "service error",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPlan(mock.Anything, "1").Return(nil, errors.New("plan not found"))
			},
			wantErr:     true,
			errContains: "plan not found",
		},
		{
			name:       "not authenticated",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))
			},
			wantErr:     true,
			errContains: "not authenticated",
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

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaPlansGetAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaPlansDelete(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().DeletePlan(mock.Anything, "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "missing plan ID",
			args:        []string{},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:       "service error",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().DeletePlan(mock.Anything, "1").Return(errors.New("cannot delete default plan"))
			},
			wantErr:     true,
			errContains: "cannot delete default plan",
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

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaPlansDeleteAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaPlansSetDefault(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			args:       []string{"2"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "2").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "missing plan ID",
			args:        []string{},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:       "service error",
			args:       []string{"999"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().SetDefaultPlan(mock.Anything, "999").Return(errors.New("plan 999 not found"))
			},
			wantErr:     true,
			errContains: "plan 999 not found",
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

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaPlansSetDefaultAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaAllowancesList(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with allowances",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListAllowances(mock.Anything).Return([]*admin.QuotaAllowance{}, 2, nil)
			},
			wantErr: false,
		},
		{
			name:       "success with empty list",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListAllowances(mock.Anything).Return([]*admin.QuotaAllowance{}, 0, nil)
			},
			wantErr: false,
		},
		{
			name:       "service error",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListAllowances(mock.Anything).Return(nil, 0, errors.New("api error"))
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

			err := quotaAllowancesListAction(context.Background(), output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaAllowancesDelete(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().DeleteAllowance(mock.Anything, "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "missing grant ID",
			args:        []string{},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "grant ID is required",
		},
		{
			name:       "service error",
			args:       []string{"1"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().DeleteAllowance(mock.Anything, "1").Return(errors.New("allowance not found"))
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

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaAllowancesDeleteAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaStats(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetStats(mock.Anything).Return(&admin.SystemStats{}, nil)
			},
			wantErr: false,
		},
		{
			name:       "json output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetStats(mock.Anything).Return(&admin.SystemStats{}, nil)
			},
			wantErr: false,
		},
		{
			name:       "service error",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetStats(mock.Anything).Return(nil, errors.New("stats unavailable"))
			},
			wantErr:     true,
			errContains: "stats unavailable",
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

			err := quotaStatsAction(context.Background(), output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaCleanup(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int64
		jsonOutput    bool
		setupMocks    func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr       bool
		errContains   string
	}{
		{
			name:          "success",
			retentionDays: 30,
			jsonOutput:    false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Cleanup(mock.Anything, int(30)).Return(50, nil)
			},
			wantErr: false,
		},
		{
			name:          "service error",
			retentionDays: 30,
			jsonOutput:    false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().Cleanup(mock.Anything, int(30)).Return(0, errors.New("cleanup failed"))
			},
			wantErr:     true,
			errContains: "cleanup failed",
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

			cmd := newMockCommand().withInt("retention-days", int(tt.retentionDays))

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaCleanupAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaUserConfigsList(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with configs",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListUserConfigs(mock.Anything).Return([]*admin.UserQuotaConfig{}, 2, nil)
			},
			wantErr: false,
		},
		{
			name:       "success with empty list",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListUserConfigs(mock.Anything).Return([]*admin.UserQuotaConfig{}, 0, nil)
			},
			wantErr: false,
		},
		{
			name:       "service error",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ListUserConfigs(mock.Anything).Return(nil, 0, errors.New("api error"))
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

			err := quotaUserConfigsListAction(context.Background(), output, cfgMgr, quotaAdminServiceFactory)

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

func TestQuotaUserConfigsReset(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			args:       []string{"100"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ResetUserPlan(mock.Anything, 100).Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "missing user ID",
			args:        []string{},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:        "invalid user ID",
			args:        []string{"invalid"},
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {},
			wantErr:     true,
			errContains: "invalid user ID",
		},
		{
			name:       "service error",
			args:       []string{"999"},
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockQuotaAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().ResetUserPlan(mock.Anything, 999).Return(errors.New("user not found"))
			},
			wantErr:     true,
			errContains: "user not found",
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

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			err := quotaUserConfigsResetAction(context.Background(), cmd, output, cfgMgr, quotaAdminServiceFactory)

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

			output := newTestOutput()

			serviceFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return service
			}

			cmd := newMockCommand()
			if len(tt.args) > 0 {
				cmd = cmd.withArgs(tt.args...)
			}

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

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{-1, "unlimited"},
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_bytes", tt.input), func(t *testing.T) {
			result := formatBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
