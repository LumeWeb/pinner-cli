package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func TestAdminPprofByteAction(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockProfilingAdminService)
		fn          func(ProfilingAdminService, context.Context) ([]byte, error)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful byte profile",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetHeapProfile(context.Background()).Return([]byte("heap-data"), nil)
			},
			fn: func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
				return svc.GetHeapProfile(ctx)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			fn: func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
				return svc.GetHeapProfile(ctx)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "returns error when service fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetHeapProfile(context.Background()).Return(nil, errors.New("profile fetch failed"))
			},
			fn: func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
				return svc.GetHeapProfile(ctx)
			},
			wantErr:     true,
			errContains: "profile fetch failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockProfilingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) ProfilingAdminService {
				return service
			}

			cmd := newMockCommand()

			err := adminPprofByteAction(context.Background(), cmd, output, cfgMgr, serviceFactory, tt.fn)

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

func TestAdminPprofSetRateAction(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		label       string
		setupMocks  func(*configmocks.MockManager, *MockProfilingAdminService)
		fn          func(ProfilingAdminService, context.Context, int) error
		wantErr     bool
		errContains string
	}{
		{
			name:  "successful set block rate",
			args:  []string{"1"},
			label: "block profile rate",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SetBlockProfileRate(context.Background(), 1).Return(nil)
			},
			fn: func(svc ProfilingAdminService, ctx context.Context, rate int) error {
				return svc.SetBlockProfileRate(ctx, rate)
			},
			wantErr: false,
		},
		{
			name:  "successful set mutex fraction",
			args:  []string{"100"},
			label: "mutex profile fraction",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SetMutexProfileFraction(context.Background(), 100).Return(nil)
			},
			fn: func(svc ProfilingAdminService, ctx context.Context, rate int) error {
				return svc.SetMutexProfileFraction(ctx, rate)
			},
			wantErr: false,
		},
		{
			name:        "returns error when rate arg is missing",
			args:        nil,
			label:       "block profile rate",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {},
			fn:          func(svc ProfilingAdminService, ctx context.Context, rate int) error { return nil },
			wantErr:     true,
			errContains: "block profile rate value is required",
		},
		{
			name:        "returns error when rate arg is not a number",
			args:        []string{"abc"},
			label:       "block profile rate",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {},
			fn:          func(svc ProfilingAdminService, ctx context.Context, rate int) error { return nil },
			wantErr:     true,
			errContains: "invalid block profile rate value: abc",
		},
		{
			name:  "returns error when not authenticated",
			args:  []string{"1"},
			label: "block profile rate",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			fn: func(svc ProfilingAdminService, ctx context.Context, rate int) error {
				return svc.SetBlockProfileRate(ctx, rate)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:  "returns error when service fails",
			args:  []string{"1"},
			label: "block profile rate",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SetBlockProfileRate(context.Background(), 1).Return(errors.New("set rate failed"))
			},
			fn: func(svc ProfilingAdminService, ctx context.Context, rate int) error {
				return svc.SetBlockProfileRate(ctx, rate)
			},
			wantErr:     true,
			errContains: "set rate failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockProfilingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) ProfilingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.args != nil {
				cmd = cmd.withArgs(tt.args...)
			}

			err := adminPprofSetRateAction(context.Background(), cmd, output, cfgMgr, serviceFactory, tt.label, tt.fn)

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

func TestAdminPprofStatusAction(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockProfilingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful status",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetStatus(context.Background()).Return(&admin.ProfilingStatus{}, nil)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "returns error when service fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockProfilingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetStatus(context.Background()).Return(nil, errors.New("status fetch failed"))
			},
			wantErr:     true,
			errContains: "status fetch failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockProfilingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) ProfilingAdminService {
				return service
			}

			cmd := newMockCommand()

			err := adminPprofStatusAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
