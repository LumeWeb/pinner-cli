package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func unmarshalPriceLineJSON(data string) *admin.PriceLine {
	var item admin.PriceLine
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func TestBillingPriceLinesList(t *testing.T) {
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
				service.EXPECT().ListPriceLines(mock.Anything).Return(
					[]*admin.PriceLine{
						unmarshalPriceLineJSON(`{"id":1,"name":"Storage","description":"Storage pricing","is_active":true,"is_default":false}`),
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
				service.EXPECT().ListPriceLines(mock.Anything).Return(
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

			err := billingPriceLinesListAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestAddPlan_AutoPosition(t *testing.T) {
	tests := []struct {
		name          string
		priceLineID   string
		planID        string
		position      int
		isSetPosition bool
		setupMocks    func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr       bool
		errContains   string
	}{
		{
			name:          "auto-position when position not set",
			priceLineID:   "1",
			planID:        "1",
			isSetPosition: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPriceLine(mock.Anything, "1").Return(&admin.PriceLineDetailResponse{
					Plans: []*admin.PricingPlanItem{{}, {}},
				}, nil)
				svc.EXPECT().AddPlanToPriceLine(mock.Anything, "1", mock.AnythingOfType("*admin.AddPlanToPriceLineRequest")).Return(&admin.PriceLineDetailResponse{}, nil)
			},
			wantErr: false,
		},
		{
			name:          "explicit position used when set",
			priceLineID:   "1",
			planID:        "1",
			position:      1,
			isSetPosition: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().AddPlanToPriceLine(mock.Anything, "1", mock.AnythingOfType("*admin.AddPlanToPriceLineRequest")).Return(&admin.PriceLineDetailResponse{}, nil)
			},
			wantErr: false,
		},
		{
			name:          "auto-position fetch fails",
			priceLineID:   "1",
			planID:        "1",
			isSetPosition: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().GetPriceLine(mock.Anything, "1").Return(nil, fmt.Errorf("%w: not found", admin.ErrNotFound))
			},
			wantErr:     true,
			errContains: "failed to determine auto-position",
		},
		{
			name:        "missing price line ID",
			priceLineID: "",
			planID:      "1",
			setupMocks:  func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockBillingAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := newTestOutput()

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}
			cmd = cmd.withString("plan-id", tt.planID)
			if tt.isSetPosition {
				cmd = cmd.withInt("position", tt.position)
				cmd = cmd.withIsSet("position", true)
			}

			err := billingPriceLinesAddPlanAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesGet(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful get",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPriceLine(mock.Anything, "1").Return(
					&admin.PriceLineDetailResponse{},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesGetAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesCreate(t *testing.T) {
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
				service.EXPECT().CreatePriceLine(mock.Anything, &admin.PriceLineCreateRequest{
					Name:        "Storage",
					Description: "Storage pricing",
					IsActive:    true,
					IsDefault:   false,
				}).Return(
					unmarshalPriceLineJSON(`{"id":1,"name":"Storage","description":"Storage pricing","is_active":true,"is_default":false}`),
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
				withString("name", "Storage").
				withString("description", "Storage pricing").
				withBool("is-active", true).
				withBool("is-default", false)

			err := billingPriceLinesCreateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesUpdate(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful update",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePriceLine(mock.Anything, "1", &admin.PriceLineUpdateRequest{
					Name:        "Updated Storage",
					Description: "Updated description",
				}).Return(
					unmarshalPriceLineJSON(`{"id":1,"name":"Updated Storage","description":"Updated description","is_active":true,"is_default":false}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}
			cmd = cmd.
				withString("name", "Updated Storage").
				withString("description", "Updated description").
				withIsSet("name", true).
				withIsSet("description", true)

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesUpdateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesDelete(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful delete",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeletePriceLine(mock.Anything, "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesDeleteAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesAddPlan(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful add plan with explicit position",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().AddPlanToPriceLine(mock.Anything, "1", &admin.AddPlanToPriceLineRequest{
					PlanId:   1,
					Position: 1,
				}).Return(
					&admin.PriceLineDetailResponse{},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}
			cmd = cmd.
				withString("plan-id", "1").
				withInt("position", 1).
				withIsSet("position", true)

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesAddPlanAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesDeletePlan(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful delete plan",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeletePlanFromPriceLine(mock.Anything, "1", "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}
			cmd = cmd.withString("plan-id", "1")

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesDeletePlanAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPriceLinesUpdatePlanPosition(t *testing.T) {
	tests := []struct {
		name        string
		priceLineID string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful update position",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePlanPosition(mock.Anything, "1", "1", &admin.UpdatePlanPositionRequest{
					Position: 2,
				}).Return(&admin.PriceLineDetailResponse{}, nil)
			},
			wantErr: false,
		},
		{
			name:        "returns error when price line ID is missing",
			priceLineID: "",
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "price line ID is required",
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
			if tt.priceLineID != "" {
				cmd = cmd.withArgs(tt.priceLineID)
			}
			cmd = cmd.
				withString("plan-id", "1").
				withInt("position", 2)

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPriceLinesUpdatePlanPositionAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
