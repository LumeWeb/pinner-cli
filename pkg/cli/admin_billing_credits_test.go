package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

// Mock command getters for billing credits tests
type mockBillingCreditsListCmd struct {
	userID     string
	direction  string
	creditType string
}

func (m *mockBillingCreditsListCmd) String(name string) string {
	switch name {
	case "user-id":
		return m.userID
	case "direction":
		return m.direction
	case "type":
		return m.creditType
	}
	return ""
}

func unmarshalCreditItemJSON(data string) *admin.CreditItem {
	var item admin.CreditItem
	json.Unmarshal([]byte(data), &item)
	return &item
}

func unmarshalCreditJSON(data string) *admin.Credit {
	var credit admin.Credit
	json.Unmarshal([]byte(data), &credit)
	return &credit
}

func unmarshalUserBalanceJSON(data string) *admin.UserBalance {
	var balance admin.UserBalance
	json.Unmarshal([]byte(data), &balance)
	return &balance
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
				service.EXPECT().ListCredits(context.Background(), &admin.GetApiBillingCreditsParams{}).Return(
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
				service.EXPECT().ListCredits(context.Background(), &admin.GetApiBillingCreditsParams{}).Return(
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := &mockBillingCreditsListCmd{}

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

// mockBillingCreditsGetCmd implements billingCreditsGetCmdGetter
type mockBillingCreditsGetCmd struct {
	args cli.Args
}

func (m *mockBillingCreditsGetCmd) Args() cli.Args {
	return m.args
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
				service.EXPECT().GetCredit(context.Background(), "123e4567-e89b-12d3-a456-426614174000").Return(
					unmarshalCreditJSON(`{"id":"123e4567-e89b-12d3-a456-426614174000","user_id":123,"amount":"100.00","type":"manual","direction":"credit"}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "returns error when credit ID is missing",
			creditID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.creditID != "" {
				args.args = []string{tt.creditID}
			}
			cmd := &mockBillingCreditsGetCmd{args: args}

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
				service.EXPECT().CreateCredit(context.Background(), &admin.CreditCreateRequest{
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := &mockBillingCreditsCreateCmd{}

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

type mockBillingCreditsCreateCmd struct {}

func (m *mockBillingCreditsCreateCmd) String(name string) string {
	switch name {
	case "user-id":
		return "123"
	case "amount":
		return "100.00"
	case "type":
		return "manual"
	case "direction":
		return "credit"
	}
	return ""
}

// Ensure mockBillingCreditsListCmd implements the interface
var _ billingCreditsListCmdGetter = (*mockBillingCreditsListCmd)(nil)
// Ensure mockBillingCreditsCreateCmd implements the interface
var _ billingCreditsCreateCmdGetter = (*mockBillingCreditsCreateCmd)(nil)
// Ensure mockBillingCreditsGetCmd implements the interface
var _ billingCreditsGetCmdGetter = (*mockBillingCreditsGetCmd)(nil)
