package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func unmarshalPriceLineJSON(data string) *admin.PriceLine {
	var item admin.PriceLine
	json.Unmarshal([]byte(data), &item)
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
				service.EXPECT().ListPriceLines(context.Background()).Return(
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
				service.EXPECT().ListPriceLines(context.Background()).Return(
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

			var cmd billingPriceLinesListCmdGetter

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
			service := NewMockBillingAdminService(t)

			tt.setupMocks(cfgMgr, service)

			output := NewOutputFormatter(false, false, false, false)

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesAddPlanArgs{
				args:          args,
				planID:        tt.planID,
				position:      tt.position,
				isSetPosition: tt.isSetPosition,
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

// billingPriceLinesGetArgs implements billingPriceLinesGetCmdGetter
type billingPriceLinesGetArgs struct {
	args cli.Args
}

func (m *billingPriceLinesGetArgs) Args() cli.Args {
	return m.args
}

func TestBillingPriceLinesGet(t *testing.T) {
	tests := []struct {
		name         string
		priceLineID  string
		setupMocks   func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr      bool
		errContains  string
	}{
		{
			name:        "successful get",
			priceLineID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetPriceLine(context.Background(), "1").Return(
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesGetArgs{args: args}

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

// billingPriceLinesCreateCmd implements billingPriceLinesCreateCmdGetter
type billingPriceLinesCreateCmd struct {
	args        cli.Args
	name        string
	description string
	isActive    bool
	isDefault   bool
}

func (m *billingPriceLinesCreateCmd) Args() cli.Args {
	return m.args
}

func (m *billingPriceLinesCreateCmd) String(name string) string {
	switch name {
	case "name":
		return m.name
	case "description":
		return m.description
	}
	return ""
}

func (m *billingPriceLinesCreateCmd) Bool(name string) bool {
	switch name {
	case "is-active":
		return m.isActive
	case "is-default":
		return m.isDefault
	}
	return false
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
				service.EXPECT().CreatePriceLine(context.Background(), &admin.PriceLineCreateRequest{
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			cmd := &billingPriceLinesCreateCmd{
				name:        "Storage",
				description: "Storage pricing",
				isActive:    true,
				isDefault:   false,
			}

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

// billingPriceLinesUpdateCmd implements billingPriceLinesUpdateCmdGetter
type billingPriceLinesUpdateCmd struct {
	args        cli.Args
	name        string
	description string
	isActive    bool
	isDefault   bool
	isSet       map[string]bool
}

func (m *billingPriceLinesUpdateCmd) Args() cli.Args {
	return m.args
}

func (m *billingPriceLinesUpdateCmd) String(name string) string {
	switch name {
	case "name":
		return m.name
	case "description":
		return m.description
	}
	return ""
}

func (m *billingPriceLinesUpdateCmd) Bool(name string) bool {
	switch name {
	case "is-active":
		return m.isActive
	case "is-default":
		return m.isDefault
	}
	return false
}

func (m *billingPriceLinesUpdateCmd) IsSet(name string) bool {
	if m.isSet == nil {
		return false
	}
	return m.isSet[name]
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
				service.EXPECT().UpdatePriceLine(context.Background(), "1", &admin.PriceLineUpdateRequest{
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesUpdateCmd{
				args:        args,
				name:        "Updated Storage",
				description: "Updated description",
				isSet: map[string]bool{
					"name":        true,
					"description": true,
				},
			}

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

// billingPriceLinesDeleteArgs implements billingPriceLinesDeleteCmdGetter
type billingPriceLinesDeleteArgs struct {
	args cli.Args
}

func (m *billingPriceLinesDeleteArgs) Args() cli.Args {
	return m.args
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
				service.EXPECT().DeletePriceLine(context.Background(), "1").Return(nil)
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesDeleteArgs{args: args}

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

// billingPriceLinesAddPlanArgs implements billingPriceLinesAddPlanCmdGetter
type billingPriceLinesAddPlanArgs struct {
	args          cli.Args
	planID        string
	position      int
	isSetPosition bool
}

func (m *billingPriceLinesAddPlanArgs) Args() cli.Args {
	return m.args
}

func (m *billingPriceLinesAddPlanArgs) String(name string) string {
	if name == "plan-id" {
		return m.planID
	}
	return ""
}

func (m *billingPriceLinesAddPlanArgs) Int(name string) int {
	switch name {
	case "plan-id":
		if m.planID != "" {
			v, _ := strconv.Atoi(m.planID)
			return v
		}
		return 0
	case "position":
		return m.position
	}
	return 0
}

func (m *billingPriceLinesAddPlanArgs) IsSet(name string) bool {
	switch name {
	case "position":
		return m.isSetPosition
	default:
		return false
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
				service.EXPECT().AddPlanToPriceLine(context.Background(), "1", &admin.AddPlanToPriceLineRequest{
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesAddPlanArgs{args: args, planID: "1", position: 1, isSetPosition: true}

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

// billingPriceLinesDeletePlanArgs implements billingPriceLinesDeletePlanCmdGetter
type billingPriceLinesDeletePlanArgs struct {
	args cli.Args
}

func (m *billingPriceLinesDeletePlanArgs) Args() cli.Args {
	return m.args
}

func (m *billingPriceLinesDeletePlanArgs) String(name string) string {
	if name == "plan-id" {
		return "1"
	}
	return ""
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
				service.EXPECT().DeletePlanFromPriceLine(context.Background(), "1", "1").Return(nil)
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesDeletePlanArgs{args: args}

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

// billingPriceLinesUpdatePlanPositionArgs implements billingPriceLinesUpdatePlanPositionCmdGetter
type billingPriceLinesUpdatePlanPositionArgs struct {
	args cli.Args
}

func (m *billingPriceLinesUpdatePlanPositionArgs) Args() cli.Args {
	return m.args
}

func (m *billingPriceLinesUpdatePlanPositionArgs) String(name string) string {
	if name == "plan-id" {
		return "1"
	}
	return ""
}

func (m *billingPriceLinesUpdatePlanPositionArgs) Int(name string) int {
	switch name {
	case "plan-id":
		return 1
	case "position":
		return 2
	}
	return 0
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
				service.EXPECT().UpdatePlanPosition(context.Background(), "1", "1", &admin.UpdatePlanPositionRequest{
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
			service := NewMockBillingAdminService(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.priceLineID != "" {
				args.args = []string{tt.priceLineID}
			}
			cmd := &billingPriceLinesUpdatePlanPositionArgs{args: args}

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
