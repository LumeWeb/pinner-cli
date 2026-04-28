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

// billingPricingPlansCreateCmd implements billingPricingPlansCreateCmdGetter
type billingPricingPlansCreateCmd struct {
	name        string
	currency    string
	description string
	isActive    bool
	isPublic    bool
	pricelineID int
	isSet       map[string]bool
}

func (m *billingPricingPlansCreateCmd) String(name string) string {
	switch name {
	case "name":
		return m.name
	case "currency":
		return m.currency
	case "description":
		return m.description
	}
	return ""
}

func (m *billingPricingPlansCreateCmd) Bool(name string) bool {
	switch name {
	case "is-active":
		return m.isActive
	case "is-public":
		return m.isPublic
	}
	return false
}

func (m *billingPricingPlansCreateCmd) Int(name string) int {
	if name == "priceline-id" {
		return m.pricelineID
	}
	return 0
}

func (m *billingPricingPlansCreateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
}

func TestBillingPricingPlansCreate(t *testing.T) {
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

			cmd := &billingPricingPlansCreateCmd{
				name:     "Pro Plan",
				currency: "USD",
				isActive: true,
			}

			err := billingPricingPlansCreateAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

func (m *billingPricingPlansUpdateCmd) String(name string) string {
	switch name {
	case "name":
		return m.name
	case "currency":
		return m.currency
	case "description":
		return m.description
	}
	return ""
}

func (m *billingPricingPlansUpdateCmd) Bool(name string) bool {
	switch name {
	case "is-active":
		return m.isActive
	case "is-public":
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
	isSet       map[string]bool
}

func (m *billingPricingPlanPeriodsCreateCmd) Int(name string) int {
	switch name {
	case "plan-id":
		return m.planID
	case "quota-plan-id":
		return m.quotaPlanID
	case "rolling-days":
		return m.rollingDays
	}
	return 0
}

func (m *billingPricingPlanPeriodsCreateCmd) Float(name string) float64 {
	if name == "price" {
		return m.price
	}
	return 0
}

func (m *billingPricingPlanPeriodsCreateCmd) String(name string) string {
	if name == "cadence" {
		return m.cadence
	}
	return ""
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
	isSet       map[string]bool
}

func (m *billingPricingPlanPeriodsUpdateCmd) Args() cli.Args {
	return m.args
}

func (m *billingPricingPlanPeriodsUpdateCmd) Float(name string) float64 {
	if name == "price" {
		return m.price
	}
	return 0
}

func (m *billingPricingPlanPeriodsUpdateCmd) String(name string) string {
	if name == "cadence" {
		return m.cadence
	}
	return ""
}

func (m *billingPricingPlanPeriodsUpdateCmd) Int(name string) int {
	switch name {
	case "quota-plan-id":
		return m.quotaPlanID
	case "rolling-days":
		return m.rollingDays
	}
	return 0
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
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful update",
			periodID: "1",
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
				args:  args,
				price: 19.99,
				isSet: map[string]bool{
					"price": true,
				},
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
