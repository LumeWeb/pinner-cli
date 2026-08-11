package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func unmarshalCreditItemJSON(data string) *admin.CreditItem {
	var item admin.CreditItem
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func unmarshalCreditJSON(data string) *admin.Credit {
	var credit admin.Credit
	if err := json.Unmarshal([]byte(data), &credit); err != nil {
		panic(err)
	}
	return &credit
}

func TestBillingCreditsList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ListCredits(mock.Anything, &admin.GetApiBillingCreditsParams{}).Return(
					[]*admin.CreditItem{
						unmarshalCreditItemJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					},
					1,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "returns error when service fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ListCredits(mock.Anything, &admin.GetApiBillingCreditsParams{}).Return(
					nil,
					0,
					errors.New("service error"),
				)
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()

			err := billingCreditsListAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsGet(t *testing.T) {
	tests := []struct {
		name        string
		creditID    string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful get",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetCredit(mock.Anything, "123e4567-e89b-12d3-a456-426614174000").Return(
					unmarshalCreditJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when credit ID is missing",
			creditID:    "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "credit ID is required",
		},
		{
			name:     "returns error when not authenticated",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.creditID != "" {
				cmd = cmd.withArgs(tt.creditID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingCreditsGetAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsDelete(t *testing.T) {
	tests := []struct {
		name        string
		creditID    string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful delete",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeleteCredit(mock.Anything, "123e4567-e89b-12d3-a456-426614174000").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "returns error when credit ID is missing",
			creditID:    "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "credit ID is required",
		},
		{
			name:     "returns error when not authenticated",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:     "returns error when service fails",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeleteCredit(mock.Anything, "123e4567-e89b-12d3-a456-426614174000").Return(errors.New("service error"))
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.creditID != "" {
				cmd = cmd.withArgs(tt.creditID)
			}

			err := billingCreditsDeleteAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsRestore(t *testing.T) {
	tests := []struct {
		name        string
		creditID    string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful restore",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().RestoreCredit(mock.Anything, "123e4567-e89b-12d3-a456-426614174000").Return(
					unmarshalCreditJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when credit ID is missing",
			creditID:    "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "credit ID is required",
		},
		{
			name:     "returns error when not authenticated",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:     "returns error when service fails",
			creditID: "123e4567-e89b-12d3-a456-426614174000",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().RestoreCredit(mock.Anything, "123e4567-e89b-12d3-a456-426614174000").Return(
					nil,
					errors.New("service error"),
				)
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.creditID != "" {
				cmd = cmd.withArgs(tt.creditID)
			}

			err := billingCreditsRestoreAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsPurge(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful purge",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().PurgeCredits(mock.Anything, &admin.CreditPurgeRequest{
					OlderThan: "30d",
				}).Return(5, nil)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "returns error when service fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().PurgeCredits(mock.Anything, &admin.CreditPurgeRequest{
					OlderThan: "30d",
				}).Return(0, errors.New("service error"))
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand().withString(FlagOlderThan, "30d")

			err := billingCreditsPurgeAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsUserBalance(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful user balance",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetUserBalance(mock.Anything, "123").Return(
					&admin.UserBalance{},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when user ID is missing",
			userID:      "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:   "returns error when not authenticated",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:   "returns error when service fails",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetUserBalance(mock.Anything, "123").Return(
					nil,
					errors.New("service error"),
				)
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.userID != "" {
				cmd = cmd.withArgs(tt.userID)
			}

			err := billingCreditsUserBalanceAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsUserDeletedCredits(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful user deleted credits",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetUserDeletedCredits(mock.Anything, "123", &admin.GetApiBillingUsersUserIdDeletedCreditsParams{}).Return(
					[]*admin.CreditItem{
						unmarshalCreditItemJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					},
					1,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when user ID is missing",
			userID:      "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:   "returns error when not authenticated",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:   "returns error when service fails",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetUserDeletedCredits(mock.Anything, "123", &admin.GetApiBillingUsersUserIdDeletedCreditsParams{}).Return(
					nil,
					0,
					errors.New("service error"),
				)
			},
			wantErr:     true,
			errContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.userID != "" {
				cmd = cmd.withArgs(tt.userID)
			}

			err := billingCreditsUserDeletedCreditsAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingCreditsCreate(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().CreateCredit(mock.Anything, &admin.CreditCreateRequest{
					UserId:    123,
					Amount:    "100.00",
					Type:      "manual",
					Direction: "credit",
				}).Return(
					unmarshalCreditJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand().
				withString(FlagUserID, "123").
				withString(FlagAmount, "100.00").
				withString(FlagType, "manual").
				withString(FlagDirection, "credit")

			err := billingCreditsCreateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
