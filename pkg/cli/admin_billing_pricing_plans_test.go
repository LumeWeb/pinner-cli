package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func unmarshalPricingPlanItemJSON(data string) *admin.PricingPlanItem {
	var item admin.PricingPlanItem
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func unmarshalPricingPlanJSON(data string) *admin.PricingPlan {
	var item admin.PricingPlan
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func unmarshalPricingPlanPeriodJSON(data string) *admin.PricingPlanPeriod {
	var item admin.PricingPlanPeriod
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func TestBillingPricingPlansGet(t *testing.T) {
	tests := []struct {
		name        string
		planID      string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful get",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPricingPlan(context.Background(), "1").Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Pro Plan","currency":"USD","is_active":true,"is_public":true,"description":"A test plan"}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "successful get with json output",
			planID:     "1",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPricingPlan(context.Background(), "1").Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Pro Plan","currency":"USD","is_active":true,"is_public":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:   "successful get with pricing periods",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPricingPlan(context.Background(), "1").Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Pro Plan","currency":"USD","is_active":true,"is_public":true,"pricing_periods":[{"id":1,"pricing_plan_id":1,"price_usd":9.99,"cadence":"monthly","quota_plan_id":1,"is_active":true}]}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:   "returns error when plan ID is missing",
			planID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
			},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:   "returns error when not authenticated",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:   "returns error when service fails",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPricingPlan(context.Background(), "1").Return(
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
			service := NewMockBillingAdminService(t)

			var output Output
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			} else {
				output = newTestOutput()
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.planID != "" {
				cmd = cmd.withArgs(tt.planID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlansGetAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlansList(t *testing.T) {
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
				service.EXPECT().ListPricingPlans(context.Background()).Return(
					[]*admin.PricingPlanItem{
						unmarshalPricingPlanItemJSON(`{"id":1,"name":"Pro Plan","currency":"USD","is_active":true}`),
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
				service.EXPECT().ListPricingPlans(context.Background()).Return(
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
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlansListAction(context.Background(), output, cfgMgr, serviceFactory)

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

func TestPricingPlanPeriodsCreate_PriceValidation(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "zero price without allow-free rejected",
			cmd: newMockCommand().
				withInt(FlagPlanID, 1).
				withFloat(FlagPrice, 0).
				withString(FlagCadence, "monthly").
				withInt(FlagQuotaPlanID, 1).
				withBool(FlagAllowFree, false),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "--price must be greater than 0",
		},
		{
			name: "zero price with allow-free accepted",
			cmd: newMockCommand().
				withInt(FlagPlanID, 1).
				withFloat(FlagPrice, 0).
				withString(FlagCadence, "monthly").
				withInt(FlagQuotaPlanID, 1).
				withBool(FlagAllowFree, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePricingPlanPeriod(mock.Anything, mock.AnythingOfType("*admin.PricingPlanPeriodCreateRequest")).Return(&admin.PricingPlanPeriod{}, nil)
			},
			wantErr: false,
		},
		{
			name: "positive price works without allow-free",
			cmd: newMockCommand().
				withInt(FlagPlanID, 1).
				withFloat(FlagPrice, 9.99).
				withString(FlagCadence, "monthly").
				withInt(FlagQuotaPlanID, 1).
				withBool(FlagAllowFree, false),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePricingPlanPeriod(mock.Anything, mock.AnythingOfType("*admin.PricingPlanPeriodCreateRequest")).Return(&admin.PricingPlanPeriod{}, nil)
			},
			wantErr: false,
		},
		{
			name: "negative price rejected",
			cmd: newMockCommand().
				withInt(FlagPlanID, 1).
				withFloat(FlagPrice, -5.0).
				withString(FlagCadence, "monthly").
				withInt(FlagQuotaPlanID, 1).
				withBool(FlagAllowFree, false),
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "--price must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := newTestOutput()

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlanPeriodsCreateAction(context.Background(), tt.cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlansCreate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create",
			cmd: newMockCommand().
				withString(FlagName, "Pro Plan").
				withString(FlagCurrency, "USD").
				withBool(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().CreatePricingPlan(context.Background(), mock.MatchedBy(func(req *admin.PricingPlanCreateRequest) bool {
					return req.Name == "Pro Plan" && req.Currency == "USD" && req.IsActive == true
				})).Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Pro Plan","currency":"USD","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			cmd: newMockCommand().
				withString(FlagName, "Pro Plan").
				withString(FlagCurrency, "USD").
				withBool(FlagIsActive, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "successful create with period",
			cmd: newMockCommand().
				withString(FlagName, "Starter Plan").
				withString(FlagCurrency, "USD").
				withBool(FlagIsActive, true).
				withInt(FlagQuotaPlanID, 1).
				withFloat(FlagPrice, 9.99).
				withString(FlagCadence, "monthly").
				withIsSet(FlagQuotaPlanID, true).
				withIsSet(FlagPrice, true).
				withIsSet(FlagCadence, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().CreatePricingPlan(context.Background(), mock.MatchedBy(func(req *admin.PricingPlanCreateRequest) bool {
					return req.Name == "Starter Plan" && req.Currency == "USD" && req.IsActive == true
				})).Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Starter Plan","currency":"USD","is_active":true}`),
					nil,
				)
				service.EXPECT().CreatePricingPlanPeriod(context.Background(), mock.MatchedBy(func(req *admin.PricingPlanPeriodCreateRequest) bool {
					return req.PricingPlanId == 1 && req.PriceUsd == 9.99 && req.Cadence == "monthly" && req.QuotaPlanId == 1
				})).Return(
					unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":9.99,"cadence":"monthly","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name: "create plan only when quota-plan-id set but price missing",
			cmd: newMockCommand().
				withString(FlagName, "Partial Plan").
				withString(FlagCurrency, "USD").
				withBool(FlagIsActive, true).
				withInt(FlagQuotaPlanID, 1).
				withIsSet(FlagQuotaPlanID, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().CreatePricingPlan(context.Background(), mock.MatchedBy(func(req *admin.PricingPlanCreateRequest) bool {
					return req.Name == "Partial Plan" && req.Currency == "USD"
				})).Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Partial Plan","currency":"USD","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlansCreateAction(context.Background(), tt.cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlansUpdate(t *testing.T) {
	tests := []struct {
		name        string
		planID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful update",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePricingPlan(context.Background(), "1", &admin.PricingPlanUpdateRequest{
					Name: "Updated Plan",
				}).Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Updated Plan","currency":"USD","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:   "returns error when plan ID is missing",
			planID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "plan ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.planID != "" {
				cmd = cmd.withArgs(tt.planID)
			}
			cmd = cmd.
				withString(FlagName, "Updated Plan").
				withIsSet(FlagName, true)

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlansUpdateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlansDelete(t *testing.T) {
	tests := []struct {
		name        string
		planID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful delete",
			planID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeletePricingPlan(context.Background(), "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:   "returns error when plan ID is missing",
			planID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "plan ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.planID != "" {
				cmd = cmd.withArgs(tt.planID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlansDeleteAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlanPeriodsList(t *testing.T) {
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
				service.EXPECT().ListPricingPlanPeriods(context.Background()).Return(
					[]*admin.PricingPlanPeriod{
						unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":9.99,"cadence":"monthly","is_active":true}`),
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
				service.EXPECT().ListPricingPlanPeriods(context.Background()).Return(
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
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlanPeriodsListAction(context.Background(), output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlanPeriodsGet(t *testing.T) {
	tests := []struct {
		name        string
		periodID    string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful get",
			periodID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPricingPlanPeriod(context.Background(), "1").Return(
					unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":9.99,"cadence":"monthly","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "returns error when period ID is missing",
			periodID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "period ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.periodID != "" {
				cmd = cmd.withArgs(tt.periodID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlanPeriodsGetAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlanPeriodsCreate(t *testing.T) {
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
				service.EXPECT().CreatePricingPlanPeriod(context.Background(), &admin.PricingPlanPeriodCreateRequest{
					PricingPlanId: 1,
					PriceUsd:      9.99,
					Cadence:       "monthly",
					QuotaPlanId:   1,
				}).Return(
					unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":9.99,"cadence":"monthly","is_active":true}`),
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
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand().
				withInt(FlagPlanID, 1).
				withFloat(FlagPrice, 9.99).
				withString(FlagCadence, "monthly").
				withInt(FlagQuotaPlanID, 1)

			err := billingPricingPlanPeriodsCreateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlanPeriodsUpdate(t *testing.T) {
	tests := []struct {
		name        string
		periodID    string
		price       float64
		allowFree   bool
		isSet       map[string]bool
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful update",
			periodID: "1",
			price:    19.99,
			isSet:    map[string]bool{"price": true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePricingPlanPeriod(context.Background(), "1", &admin.PricingPlanPeriodUpdateRequest{
					PriceUsd: 19.99,
				}).Return(
					unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":19.99,"cadence":"monthly","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "allow-free sets AllowFree on request",
			periodID:  "1",
			price:     0,
			allowFree: true,
			isSet:     map[string]bool{"allow-free": true},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePricingPlanPeriod(context.Background(), "1", mock.MatchedBy(func(req *admin.PricingPlanPeriodUpdateRequest) bool {
					return req.AllowFree != nil && *req.AllowFree == true && req.PriceUsd == 0
				})).Return(
					unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"price_usd":0,"cadence":"monthly","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:     "returns error when period ID is missing",
			periodID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "period ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.periodID != "" {
				cmd = cmd.withArgs(tt.periodID)
			}
			cmd = cmd.
				withFloat(FlagPrice, tt.price).
				withBool(FlagAllowFree, tt.allowFree)
			for k, v := range tt.isSet {
				cmd = cmd.withIsSet(k, v)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlanPeriodsUpdateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingPricingPlanPeriodsDelete(t *testing.T) {
	tests := []struct {
		name        string
		periodID    string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful delete",
			periodID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeletePricingPlanPeriod(context.Background(), "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:     "returns error when period ID is missing",
			periodID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "period ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			cmd := newMockCommand()
			if tt.periodID != "" {
				cmd = cmd.withArgs(tt.periodID)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingPricingPlanPeriodsDeleteAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingSyncPricingPlan(t *testing.T) {
	tests := []struct {
		name        string
		planID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful sync",
			planID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SyncPricingPlan(context.Background(), "123").Return(nil)
			},
			wantErr: false,
		},
		{
			name:   "returns error when plan ID is missing",
			planID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
			},
			wantErr:     true,
			errContains: "plan ID is required",
		},
		{
			name:   "returns error when not authenticated",
			planID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:   "returns error when service fails",
			planID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SyncPricingPlan(context.Background(), "123").Return(errors.New("sync failed"))
			},
			wantErr:     true,
			errContains: "sync failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := newMockCommand()
			if tt.planID != "" {
				cmd = cmd.withArgs(tt.planID)
			}

			err := billingSyncPricingPlanAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func TestBillingSyncAllPricingPlans(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful sync all",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().SyncAllPricingPlans(context.Background()).Return(nil)
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
				service.EXPECT().SyncAllPricingPlans(context.Background()).Return(errors.New("sync all failed"))
			},
			wantErr:     true,
			errContains: "sync all failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			service := NewMockBillingAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingSyncAllPricingPlansAction(context.Background(), output, cfgMgr, serviceFactory)

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
