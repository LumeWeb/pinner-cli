package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func unmarshalSubscriberJSON(data string) *admin.Subscriber {
	var item admin.Subscriber
	json.Unmarshal([]byte(data), &item)
	return &item
}



func TestBillingSubscribersList(t *testing.T) {
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
				service.EXPECT().ListSubscribers(context.Background()).Return(
					[]*admin.Subscriber{
						{
							Id:             1,
							UserId:         123,
							ExternalId:     "ext-123",
							GatewayType:    "stripe",
							SubscriptionId: "sub-123",
							IsActive:       true,
						},
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
				service.EXPECT().ListSubscribers(context.Background()).Return(
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

			var cmd billingSubscribersListCmdGetter

			err := billingSubscribersListAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersGetArgs implements billingSubscribersGetCmdGetter
type billingSubscribersGetArgs struct {
	args cli.Args
}

func (m *billingSubscribersGetArgs) Args() cli.Args {
	return m.args
}

func TestBillingSubscribersGet(t *testing.T) {
	tests := []struct {
		name         string
		subscriberID string
		setupMocks   func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr      bool
		errContains  string
	}{
		{
			name:         "successful get",
			subscriberID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetSubscriber(context.Background(), "1").Return(
					unmarshalSubscriberJSON(`{"id":1,"user_id":123,"external_id":"ext-123","gateway_type":"stripe","subscription_id":"sub-123","is_active":true}`),
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:         "returns error when subscriber ID is missing",
			subscriberID: "",
			setupMocks:   func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:      true,
			errContains:  "subscriber ID is required",
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
			if tt.subscriberID != "" {
				args.args = []string{tt.subscriberID}
			}
			cmd := &billingSubscribersGetArgs{args: args}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingSubscribersGetAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersListGatewayArgs implements billingSubscribersListGatewayCmdGetter
type billingSubscribersListGatewayArgs struct {
	args cli.Args
}

func (m *billingSubscribersListGatewayArgs) Args() cli.Args {
	return m.args
}

func TestBillingSubscribersListGateway(t *testing.T) {
	tests := []struct {
		name        string
		gatewayID   string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful list gateway",
			gatewayID: "stripe",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ListGatewaySubscribers(context.Background(), "stripe").Return(
					[]*admin.Subscriber{
						unmarshalSubscriberJSON(`{"id":1,"user_id":123,"external_id":"ext-123","gateway_type":"stripe","subscription_id":"sub-123","is_active":true}`),
					},
					1,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "returns error when gateway ID is missing",
			gatewayID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "gateway ID is required",
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
			if tt.gatewayID != "" {
				args.args = []string{tt.gatewayID}
			}
			cmd := &billingSubscribersListGatewayArgs{args: args}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingSubscribersListGatewayAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersListUserArgs implements billingSubscribersListUserCmdGetter
type billingSubscribersListUserArgs struct {
	args cli.Args
}

func (m *billingSubscribersListUserArgs) Args() cli.Args {
	return m.args
}

func TestBillingSubscribersListUser(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful list user",
			userID: "123",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().GetUserSubscribers(context.Background(), "123").Return(
					[]*admin.Subscriber{
						unmarshalSubscriberJSON(`{"id":1,"user_id":123,"external_id":"ext-123","gateway_type":"stripe","subscription_id":"sub-123","is_active":true}`),
					},
					1,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:   "returns error when user ID is missing",
			userID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {},
			wantErr:     true,
			errContains: "user ID is required",
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
			if tt.userID != "" {
				args.args = []string{tt.userID}
			}
			cmd := &billingSubscribersListUserArgs{args: args}

			serviceFactory := func(cm config.Manager, out Output) BillingAdminService {
				return service
			}

			err := billingSubscribersListUserAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersCancelCmd implements billingSubscribersCancelCmdGetter
type billingSubscribersCancelCmd struct {
	userID string
	mode   string
}

func (m *billingSubscribersCancelCmd) String(name string) string {
	switch name {
	case FlagUserID:
		return m.userID
	case FlagMode:
		return m.mode
	}
	return ""
}

func TestBillingSubscribersCancel(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful cancel",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				mode := "end_of_billing_period"
				service.EXPECT().CancelUserSubscription(context.Background(), "123", &admin.CancelSubscriptionRequest{
					Mode: &mode,
				}).Return(
					&admin.ManagementResult{
						Action: "cancel",
					},
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

			cmd := &billingSubscribersCancelCmd{
				userID: "123",
				mode:   "end_of_billing_period",
			}

			err := billingSubscribersCancelAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersAbortCancelCmd implements billingSubscribersAbortCancelCmdGetter
type billingSubscribersAbortCancelCmd struct {
	userID string
}

func (m *billingSubscribersAbortCancelCmd) String(name string) string {
	if name == FlagUserID {
		return m.userID
	}
	return ""
}

func TestBillingSubscribersAbortCancel(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful abort cancel",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().AbortUserSubscriptionCancellation(context.Background(), "123").Return(
					&admin.ManagementResult{
						Action: "abort_cancel",
					},
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

			cmd := &billingSubscribersAbortCancelCmd{
				userID: "123",
			}

			err := billingSubscribersAbortCancelAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersChangePlanCmd implements billingSubscribersChangePlanCmdGetter
type billingSubscribersChangePlanCmd struct {
	userID   string
	periodID int
}

func (m *billingSubscribersChangePlanCmd) String(name string) string {
	if name == FlagUserID {
		return m.userID
	}
	return ""
}

func (m *billingSubscribersChangePlanCmd) Int(name string) int {
	if name == FlagPlanID {
		return m.periodID
	}
	return 0
}

func TestBillingSubscribersChangePlan(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful change plan",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ChangeUserPlan(context.Background(), "123", &admin.ChangePlanRequest{
					PeriodId: 1,
				}).Return(
					&admin.PlanChangeResult{
						Action:       "change_plan",
						ChargeDue:    decimal.RequireFromString("0.00"),
						CreditApplied: decimal.RequireFromString("0.00"),
					},
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

			cmd := &billingSubscribersChangePlanCmd{
				userID:   "123",
				periodID: 1,
			}

			err := billingSubscribersChangePlanAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersPauseCmd implements billingSubscribersPauseCmdGetter
type billingSubscribersPauseCmd struct {
	userID string
}

func (m *billingSubscribersPauseCmd) String(name string) string {
	if name == FlagUserID {
		return m.userID
	}
	return ""
}

func TestBillingSubscribersPause(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful pause",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().PauseUserSubscription(context.Background(), "123").Return(
					&admin.ManagementResult{
						Action: "pause",
					},
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

			cmd := &billingSubscribersPauseCmd{
				userID: "123",
			}

			err := billingSubscribersPauseAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

// billingSubscribersResumeCmd implements billingSubscribersResumeCmdGetter
type billingSubscribersResumeCmd struct {
	userID string
}

func (m *billingSubscribersResumeCmd) String(name string) string {
	if name == FlagUserID {
		return m.userID
	}
	return ""
}

func TestBillingSubscribersResume(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockBillingAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful resume",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockBillingAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ResumeUserSubscription(context.Background(), "123").Return(
					&admin.ManagementResult{
						Action: "resume",
					},
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

			cmd := &billingSubscribersResumeCmd{
				userID: "123",
			}

			err := billingSubscribersResumeAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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
