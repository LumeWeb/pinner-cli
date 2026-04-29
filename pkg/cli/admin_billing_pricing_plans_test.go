package cli

import (
	"context"
	"encoding/json"
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

func unmarshalPricingPlanItemJSON(data string) *admin.PricingPlanItem {
	var item admin.PricingPlanItem
	json.Unmarshal([]byte(data), &item)
	return &item
}

func unmarshalPricingPlanJSON(data string) *admin.PricingPlan {
	var item admin.PricingPlan
	json.Unmarshal([]byte(data), &item)
	return &item
}

func unmarshalPricingPlanPeriodJSON(data string) *admin.PricingPlanPeriod {
	var item admin.PricingPlanPeriod
	json.Unmarshal([]byte(data), &item)
	return &item
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			var cmd billingPricingPlansListCmdGetter

			err := billingPricingPlansListAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
		cmd         *billingPricingPlanPeriodsCreateCmd
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "zero price without allow-free rejected",
			cmd: &billingPricingPlanPeriodsCreateCmd{
				planID:      1,
				price:       0,
				cadence:     "monthly",
				quotaPlanID: 1,
				allowFree:   false,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "--price must be greater than 0",
		},
		{
			name: "zero price with allow-free accepted",
			cmd: &billingPricingPlanPeriodsCreateCmd{
				planID:      1,
				price:       0,
				cadence:     "monthly",
				quotaPlanID: 1,
				allowFree:   true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePricingPlanPeriod(mock.Anything, mock.AnythingOfType("*admin.PricingPlanPeriodCreateRequest")).Return(&admin.PricingPlanPeriod{}, nil)
			},
			wantErr: false,
		},
		{
			name: "positive price works without allow-free",
			cmd: &billingPricingPlanPeriodsCreateCmd{
				planID:      1,
				price:       9.99,
				cadence:     "monthly",
				quotaPlanID: 1,
				allowFree:   false,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, svc *MockBillingAdminService) {
				svc.EXPECT().RequireAuthenticated().Return(nil)
				svc.EXPECT().CreatePricingPlanPeriod(mock.Anything, mock.AnythingOfType("*admin.PricingPlanPeriodCreateRequest")).Return(&admin.PricingPlanPeriod{}, nil)
			},
			wantErr: false,
		},
		{
			name: "negative price rejected",
			cmd: &billingPricingPlanPeriodsCreateCmd{
				planID:      1,
				price:       -5.0,
				cadence:     "monthly",
				quotaPlanID: 1,
				allowFree:   false,
			},
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

			output := NewOutputFormatter(false, false, false, false)

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

// billingPricingPlansCreateCmd implements billingPricingPlansCreateCmdGetter
type billingPricingPlansCreateCmd struct {
	name        string
	currency    string
	description string
	isActive    bool
	isPublic    bool
	pricelineID int
	// Period creation fields
	price       float64
	cadence     string
	quotaPlanID int
	rollingDays int
	allowFree   bool
	isSet       map[string]bool
}

func (m *billingPricingPlansCreateCmd) String(name string) string {
	switch name {
	case FlagName:
		return m.name
	case FlagCurrency:
		return m.currency
	case FlagDescription:
		return m.description
	case FlagCadence:
		return m.cadence
	}
	return ""
}

func (m *billingPricingPlansCreateCmd) Bool(name string) bool {
	switch name {
	case FlagIsActive:
		return m.isActive
	case FlagIsPublic:
		return m.isPublic
	case FlagAllowFree:
		return m.allowFree
	}
	return false
}

func (m *billingPricingPlansCreateCmd) Int(name string) int {
	switch name {
	case FlagPricelineID:
		return m.pricelineID
	case FlagQuotaPlanID:
		return m.quotaPlanID
	case FlagRollingDays:
		return m.rollingDays
	}
	return 0
}

func (m *billingPricingPlansCreateCmd) Float(name string) float64 {
	if name == FlagPrice {
		return m.price
	}
	return 0
}

func (m *billingPricingPlansCreateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
}

func (m *billingPricingPlansUpdateCmd) String(name string) string {
	switch name {
	case FlagName:
		return m.name
	case FlagCurrency:
		return m.currency
	case FlagDescription:
		return m.description
	}
	return ""
}

func (m *billingPricingPlansUpdateCmd) Bool(name string) bool {
	switch name {
	case FlagIsActive:
		return m.isActive
	case FlagIsPublic:
		return m.isPublic
	}
	return false
}

func (m *billingPricingPlansUpdateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
}

func TestBillingPricingPlansCreate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *billingPricingPlansCreateCmd
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create",
			cmd: &billingPricingPlansCreateCmd{
				name:     "Pro Plan",
				currency: "USD",
				isActive: true,
			},
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
			cmd: &billingPricingPlansCreateCmd{
				name:     "Pro Plan",
				currency: "USD",
				isActive: true,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "successful create with period",
			cmd: &billingPricingPlansCreateCmd{
				name:        "Starter Plan",
				currency:    "USD",
				isActive:    true,
				quotaPlanID: 1,
				price:       9.99,
				cadence:     "monthly",
				isSet: map[string]bool{
					FlagQuotaPlanID: true,
					FlagPrice:       true,
					FlagCadence:     true,
				},
			},
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
			cmd: &billingPricingPlansCreateCmd{
				name:        "Partial Plan",
				currency:    "USD",
				isActive:    true,
				quotaPlanID: 1,
				isSet: map[string]bool{
					FlagQuotaPlanID: true,
				},
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().CreatePricingPlan(context.Background(), mock.MatchedBy(func(req *admin.PricingPlanCreateRequest) bool {
					return req.Name == "Partial Plan" && req.Currency == "USD"
				})).Return(
					unmarshalPricingPlanJSON(`{"id":1,"name":"Partial Plan","currency":"USD","is_active":true}`),
					nil,
				)
				// Period creation skipped because price/cadence not set
			},
			wantErr: false,
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

// billingPricingPlansUpdateCmd implements billingPricingPlansUpdateCmdGetter
type billingPricingPlansUpdateCmd struct {
	args        cli.Args
	name        string
	currency    string
	description string
	isActive    bool
	isPublic    bool
	isSet       map[string]bool
}

func (m *billingPricingPlansUpdateCmd) Args() cli.Args {
	return m.args
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.planID != "" {
				args.args = []string{tt.planID}
			}
			cmd := &billingPricingPlansUpdateCmd{
				args: args,
				name: "Updated Plan",
				isSet: map[string]bool{
					"name": true,
				},
			}

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

// billingPricingPlansDeleteArgs implements billingPricingPlansDeleteCmdGetter
type billingPricingPlansDeleteArgs struct {
	args cli.Args
}

func (m *billingPricingPlansDeleteArgs) Args() cli.Args {
	return m.args
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.planID != "" {
				args.args = []string{tt.planID}
			}
			cmd := &billingPricingPlansDeleteArgs{args: args}

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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			var cmd billingPricingPlanPeriodsListCmdGetter

			err := billingPricingPlanPeriodsListAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingPricingPlanPeriodsGetArgs implements billingPricingPlanPeriodsGetCmdGetter
type billingPricingPlanPeriodsGetArgs struct {
	args cli.Args
}

func (m *billingPricingPlanPeriodsGetArgs) Args() cli.Args {
	return m.args
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.periodID != "" {
				args.args = []string{tt.periodID}
			}
			cmd := &billingPricingPlanPeriodsGetArgs{args: args}

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

// billingPricingPlanPeriodsCreateCmd implements billingPricingPlanPeriodsCreateCmdGetter
type billingPricingPlanPeriodsCreateCmd struct {
	planID      int
	planIDStr   string
	price       float64
	cadence     string
	quotaPlanID int
	rollingDays int
	allowFree   bool
	isSet       map[string]bool
}

func (m *billingPricingPlanPeriodsCreateCmd) Int(name string) int {
	switch name {
	case FlagPlanID:
		return m.planID
	case FlagQuotaPlanID:
		return m.quotaPlanID
	case FlagRollingDays:
		return m.rollingDays
	}
	return 0
}

func (m *billingPricingPlanPeriodsCreateCmd) Float(name string) float64 {
	if name == FlagPrice {
		return m.price
	}
	return 0
}

func (m *billingPricingPlanPeriodsCreateCmd) String(name string) string {
	if name == FlagCadence {
		return m.cadence
	}
	return ""
}

func (m *billingPricingPlanPeriodsCreateCmd) Bool(name string) bool {
	if name == FlagAllowFree {
		return m.allowFree
	}
	return false
}

func (m *billingPricingPlanPeriodsCreateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := &billingPricingPlanPeriodsCreateCmd{
				planID:      1,
				price:       9.99,
				cadence:     "monthly",
				quotaPlanID: 1,
			}

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

// billingPricingPlanPeriodsUpdateCmd implements billingPricingPlanPeriodsUpdateCmdGetter
type billingPricingPlanPeriodsUpdateCmd struct {
	args        cli.Args
	price       float64
	cadence     string
	quotaPlanID int
	rollingDays int
	allowFree   bool
	isSet       map[string]bool
}

func (m *billingPricingPlanPeriodsUpdateCmd) Args() cli.Args {
	return m.args
}

func (m *billingPricingPlanPeriodsUpdateCmd) Float(name string) float64 {
	if name == FlagPrice {
		return m.price
	}
	return 0
}

func (m *billingPricingPlanPeriodsUpdateCmd) String(name string) string {
	if name == FlagCadence {
		return m.cadence
	}
	return ""
}

func (m *billingPricingPlanPeriodsUpdateCmd) Int(name string) int {
	switch name {
	case FlagQuotaPlanID:
		return m.quotaPlanID
	case FlagRollingDays:
		return m.rollingDays
	}
	return 0
}

func (m *billingPricingPlanPeriodsUpdateCmd) Bool(name string) bool {
	if name == FlagAllowFree {
		return m.allowFree
	}
	return false
}

func (m *billingPricingPlanPeriodsUpdateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
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
			name:     "allow-free sets AllowFree on request",
			periodID: "1",
			price:    0,
			allowFree: true,
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.periodID != "" {
				args.args = []string{tt.periodID}
			}
			cmd := &billingPricingPlanPeriodsUpdateCmd{
				args:      args,
				price:     tt.price,
				allowFree: tt.allowFree,
				isSet:     tt.isSet,
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

// billingPricingPlanPeriodsDeleteArgs implements billingPricingPlanPeriodsDeleteCmdGetter
type billingPricingPlanPeriodsDeleteArgs struct {
	args cli.Args
}

func (m *billingPricingPlanPeriodsDeleteArgs) Args() cli.Args {
	return m.args
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.periodID != "" {
				args.args = []string{tt.periodID}
			}
			cmd := &billingPricingPlanPeriodsDeleteArgs{args: args}

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

// Mock command getters for sync commands
type mockBillingSyncCmd struct {
	args cli.Args
}

func (m *mockBillingSyncCmd) Args() cli.Args {
	return m.args
}

// mockBillingSyncAllCmd is an empty struct for sync-all command (no args needed)
type mockBillingSyncAllCmd struct{}

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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			args := &mockArgs{}
			if tt.planID != "" {
				args.args = []string{tt.planID}
			}
			cmd := &mockBillingSyncCmd{args: args}

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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := &mockBillingSyncAllCmd{}

			err := billingSyncAllPricingPlansAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
