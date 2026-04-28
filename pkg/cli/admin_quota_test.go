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
	"go.lumeweb.com/portal-sdk/admin"
)

// Mock command getters for quota tests
type mockQuotaPlansGetCmd struct {
	args cli.Args
}

func (m *mockQuotaPlansGetCmd) Args() cli.Args {
	return m.args
}

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

			// Use an empty struct that implements quotaPlansListCmdGetter
			err := quotaPlansListAction(context.Background(), struct{}{}, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			args := &mockArgs{}
			if len(tt.args) > 0 {
				args.args = tt.args
			}
			cmd := &mockQuotaPlansGetCmd{args: args}

			err := quotaPlansGetAction(context.Background(), cmd, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			args := &mockArgs{}
			if len(tt.args) > 0 {
				args.args = tt.args
			}
			cmd := &mockQuotaPlansGetCmd{args: args}

			err := quotaPlansDeleteAction(context.Background(), cmd, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			args := &mockArgs{}
			if len(tt.args) > 0 {
				args.args = tt.args
			}
			cmd := &mockQuotaPlansGetCmd{args: args}

			err := quotaPlansSetDefaultAction(context.Background(), cmd, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			err := quotaAllowancesListAction(context.Background(), struct{}{}, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			args := &mockArgs{}
			if len(tt.args) > 0 {
				args.args = tt.args
			}
			cmd := &mockQuotaPlansGetCmd{args: args}

			err := quotaAllowancesDeleteAction(context.Background(), cmd, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			err := quotaStatsAction(context.Background(), struct{}{}, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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
		name           string
		retentionDays  int64
		jsonOutput     bool
		setupMocks     func(*configmocks.MockManager, *MockQuotaAdminService)
		wantErr        bool
		errContains    string
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

			cmdWrapper := &cleanupCmdMock{retentionDays: int(tt.retentionDays)}

			err := quotaCleanupAction(context.Background(), cmdWrapper, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

type cleanupCmdMock struct {
	retentionDays int
}

func (c *cleanupCmdMock) Int(name string) int {
	if name == "retention-days" {
		return c.retentionDays
	}
	return 0
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

			err := quotaUserConfigsListAction(context.Background(), struct{}{}, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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

			args := &mockArgs{}
			if len(tt.args) > 0 {
				args.args = tt.args
			}
			cmd := &mockQuotaPlansGetCmd{args: args}

			err := quotaUserConfigsResetAction(context.Background(), cmd, NewOutputFormatter(tt.jsonOutput, false, false, false), cfgMgr, quotaAdminServiceFactory)

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
