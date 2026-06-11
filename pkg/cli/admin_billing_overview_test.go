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

func unmarshalQuotaPlanJSON(data string) *admin.QuotaPlan {
	var item admin.QuotaPlan
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		panic(err)
	}
	return &item
}

func TestBillingOverview(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService, *MockQuotaAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful overview",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					[]*admin.QuotaPlan{
						unmarshalQuotaPlanJSON(`{"id":1,"name":"Free","is_active":true}`),
						unmarshalQuotaPlanJSON(`{"id":2,"name":"Pro","is_active":false}`),
					},
					2,
					nil,
				)
				billingSvc.EXPECT().ListPriceLines(mock.Anything).Return(
					[]*admin.PriceLine{
						unmarshalPriceLineJSON(`{"id":1,"name":"Storage","is_active":true}`),
					},
					1,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlans(mock.Anything).Return(
					[]*admin.PricingPlanItem{
						unmarshalPricingPlanItemJSON(`{"id":1,"name":"Monthly","is_active":true}`),
					},
					1,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlanPeriods(mock.Anything).Return(
					[]*admin.PricingPlanPeriod{
						unmarshalPricingPlanPeriodJSON(`{"id":1,"pricing_plan_id":1,"quota_plan_id":1}`),
					},
					1,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "successful overview json output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					[]*admin.QuotaPlan{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPriceLines(mock.Anything).Return(
					[]*admin.PriceLine{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlans(mock.Anything).Return(
					[]*admin.PricingPlanItem{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlanPeriods(mock.Anything).Return(
					[]*admin.PricingPlanPeriod{},
					0,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:       "returns error when billing not authenticated",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:       "returns error when quota service fails",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					nil,
					0,
					errors.New("quota api error"),
				)
			},
			wantErr:     true,
			errContains: "failed to list quota plans",
		},
		{
			name:       "returns error when billing list price lines fails",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					[]*admin.QuotaPlan{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPriceLines(mock.Anything).Return(
					nil,
					0,
					errors.New("price lines api error"),
				)
			},
			wantErr:     true,
			errContains: "failed to list price lines",
		},
		{
			name:       "returns error when billing list pricing plans fails",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					[]*admin.QuotaPlan{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPriceLines(mock.Anything).Return(
					[]*admin.PriceLine{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlans(mock.Anything).Return(
					nil,
					0,
					errors.New("pricing plans api error"),
				)
			},
			wantErr:     true,
			errContains: "failed to list pricing plans",
		},
		{
			name:       "returns error when billing list pricing plan periods fails",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, billingSvc *MockBillingAdminService, quotaSvc *MockQuotaAdminService) {
				billingSvc.EXPECT().RequireAuthenticated().Return(nil)
				quotaSvc.EXPECT().ListPlans(mock.Anything).Return(
					[]*admin.QuotaPlan{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPriceLines(mock.Anything).Return(
					[]*admin.PriceLine{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlans(mock.Anything).Return(
					[]*admin.PricingPlanItem{},
					0,
					nil,
				)
				billingSvc.EXPECT().ListPricingPlanPeriods(mock.Anything).Return(
					nil,
					0,
					errors.New("periods api error"),
				)
			},
			wantErr:     true,
			errContains: "failed to list pricing plan periods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			billingSvc := NewMockBillingAdminService(t)
			quotaSvc := NewMockQuotaAdminService(t)
			output := newTestOutput()
			if tt.jsonOutput {
				output = NewOutputFormatter(true, false, false, false)
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, billingSvc, quotaSvc)
			}

			billingFactory := func(cm config.Manager, out Output) BillingAdminService {
				return billingSvc
			}
			quotaFactory := func(cm config.Manager, out Output) QuotaAdminService {
				return quotaSvc
			}

			err := billingOverviewAction(context.Background(), output, cfgMgr, billingFactory, quotaFactory)

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
