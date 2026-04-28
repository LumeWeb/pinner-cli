package cli

import (
	"context"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// quotaAdminService implements the QuotaAdminService interface using the admin.QuotaService.
type quotaAdminService struct {
	service       *admin.QuotaService
	cfgMgr        config.Manager
	authToken     string
	authenticated bool
}

// billingAdminService implements the BillingAdminService interface using the admin.BillingService.
type billingAdminService struct {
	service       *admin.BillingService
	cfgMgr        config.Manager
	authToken     string
	authenticated bool
}

// QuotaAdminServiceFactory creates a QuotaAdminService with dependencies.
type QuotaAdminServiceFactory func(cfgMgr config.Manager, output Output) QuotaAdminService

// BillingAdminServiceFactory creates a BillingAdminService with dependencies.
type BillingAdminServiceFactory func(cfgMgr config.Manager, output Output) BillingAdminService

// defaultQuotaAdminServiceFactory creates a default QuotaAdminService instance.
func defaultQuotaAdminServiceFactory(cfgMgr config.Manager, output Output) QuotaAdminService {
	return NewQuotaAdminService(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
}

// defaultBillingAdminServiceFactory creates a default BillingAdminService instance.
func defaultBillingAdminServiceFactory(cfgMgr config.Manager, output Output) BillingAdminService {
	return NewBillingAdminService(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
}

// NewQuotaAdminService creates a new QuotaAdminService instance.
func NewQuotaAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) QuotaAdminService {
	authToken := cfgMgr.Config().AuthToken

	client := admin.NewClient(
		admin.WithEndpoint(apiEndpoint),
		admin.WithJWT(authToken),
	)

	return &quotaAdminService{
		service:       client.Quota(),
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// NewBillingAdminService creates a new BillingAdminService instance.
func NewBillingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) BillingAdminService {
	authToken := cfgMgr.Config().AuthToken

	client := admin.NewClient(
		admin.WithEndpoint(apiEndpoint),
		admin.WithJWT(authToken),
	)

	return &billingAdminService{
		service:       client.Billing(),
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// QuotaAdminService defines the interface for quota admin operations.
type QuotaAdminService interface {
	RequireAuthenticated() error

	// Plan operations
	ListPlans(ctx context.Context) ([]*admin.QuotaPlan, int, error)
	CreatePlan(ctx context.Context, plan *admin.QuotaPlan) (*admin.QuotaPlan, error)
	GetPlan(ctx context.Context, planID string) (*admin.QuotaPlan, error)
	UpdatePlan(ctx context.Context, planID string, plan *admin.QuotaPlan) (*admin.QuotaPlan, error)
	DeletePlan(ctx context.Context, planID string) error
	SetDefaultPlan(ctx context.Context, planID string) error

	// Allowance operations
	ListAllowances(ctx context.Context) ([]*admin.QuotaAllowance, int, error)
	CreateAllowance(ctx context.Context, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error)
	UpdateAllowance(ctx context.Context, grantID string, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error)
	DeleteAllowance(ctx context.Context, grantID string) error

	// System operations
	GetStats(ctx context.Context) (*admin.SystemStats, error)
	Reconcile(ctx context.Context, userID *int) (string, int, error)
	Cleanup(ctx context.Context, retentionDays int) (int, error)

	// User config operations
	ListUserConfigs(ctx context.Context) ([]*admin.UserQuotaConfig, int, error)
	UpdateUserConfig(ctx context.Context, userID int, config *admin.UserQuotaConfigUpdate) (*admin.UserQuotaConfig, error)
	ResetUserPlan(ctx context.Context, userID int) error
}

// BillingAdminService defines the interface for billing admin operations.
type BillingAdminService interface {
	RequireAuthenticated() error

	// Credit operations
	ListCredits(ctx context.Context, params *admin.GetApiBillingCreditsParams) ([]*admin.CreditItem, int, error)
	CreateCredit(ctx context.Context, req *admin.CreditCreateRequest) (*admin.Credit, error)
	GetCredit(ctx context.Context, creditID string) (*admin.Credit, error)
	DeleteCredit(ctx context.Context, creditID string) error
	RestoreCredit(ctx context.Context, creditID string) (*admin.Credit, error)
	PurgeCredits(ctx context.Context, req *admin.CreditPurgeRequest) (int, error)

	// User balance operations
	GetUserBalance(ctx context.Context, userID string) (*admin.UserBalance, error)
	GetUserDeletedCredits(ctx context.Context, userID string, params *admin.GetApiBillingUsersUserIdDeletedCreditsParams) ([]*admin.CreditItem, int, error)

	// Price line operations
	ListPriceLines(ctx context.Context) ([]*admin.PriceLine, int, error)
	CreatePriceLine(ctx context.Context, req *admin.PriceLineCreateRequest) (*admin.PriceLine, error)
	GetPriceLine(ctx context.Context, priceLineID string) (*admin.PriceLineDetailResponse, error)
	UpdatePriceLine(ctx context.Context, priceLineID string, req *admin.PriceLineUpdateRequest) (*admin.PriceLine, error)
	DeletePriceLine(ctx context.Context, priceLineID string) error

	// Pricing plan operations
	ListPricingPlans(ctx context.Context) ([]*admin.PricingPlanItem, int, error)
	CreatePricingPlan(ctx context.Context, req *admin.PricingPlanCreateRequest) (*admin.PricingPlan, error)
	UpdatePricingPlan(ctx context.Context, planID string, req *admin.PricingPlanUpdateRequest) (*admin.PricingPlan, error)
	DeletePricingPlan(ctx context.Context, planID string) error

	// Pricing plan period operations
	ListPricingPlanPeriods(ctx context.Context) ([]*admin.PricingPlanPeriod, int, error)
	CreatePricingPlanPeriod(ctx context.Context, req *admin.PricingPlanPeriodCreateRequest) (*admin.PricingPlanPeriod, error)
	GetPricingPlanPeriod(ctx context.Context, periodID string) (*admin.PricingPlanPeriod, error)
	UpdatePricingPlanPeriod(ctx context.Context, periodID string, req *admin.PricingPlanPeriodUpdateRequest) (*admin.PricingPlanPeriod, error)
	DeletePricingPlanPeriod(ctx context.Context, periodID string) error

	// Subscriber operations
	ListSubscribers(ctx context.Context) ([]*admin.Subscriber, int, error)
	GetSubscriber(ctx context.Context, subscriberID string) (*admin.Subscriber, error)
	ListGatewaySubscribers(ctx context.Context, gatewayID string) ([]*admin.Subscriber, int, error)
	GetUserSubscribers(ctx context.Context, userID string) ([]*admin.Subscriber, int, error)

	// Subscription management operations
	CancelUserSubscription(ctx context.Context, userID string, req *admin.CancelSubscriptionRequest) (*admin.ManagementResult, error)
	AbortUserSubscriptionCancellation(ctx context.Context, userID string) (*admin.ManagementResult, error)
	ChangeUserPlan(ctx context.Context, userID string, req *admin.ChangePlanRequest) (*admin.PlanChangeResult, error)
	PauseUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error)
	ResumeUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error)

	// Price line plan operations
	AddPlanToPriceLine(ctx context.Context, priceLineID string, req *admin.AddPlanToPriceLineRequest) (*admin.PriceLineDetailResponse, error)
	DeletePlanFromPriceLine(ctx context.Context, priceLineID, planID string) error
	UpdatePlanPosition(ctx context.Context, priceLineID, planID string, req *admin.UpdatePlanPositionRequest) (*admin.PriceLineDetailResponse, error)
}

// RequireAuthenticated checks if the quota admin service is authenticated.
func (s *quotaAdminService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}

// ListPlans lists all quota plans.
func (s *quotaAdminService) ListPlans(ctx context.Context) ([]*admin.QuotaPlan, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListPlans(ctx)
}

// CreatePlan creates a new quota plan.
func (s *quotaAdminService) CreatePlan(ctx context.Context, plan *admin.QuotaPlan) (*admin.QuotaPlan, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreatePlan(ctx, plan)
}

// GetPlan retrieves a quota plan by ID.
func (s *quotaAdminService) GetPlan(ctx context.Context, planID string) (*admin.QuotaPlan, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetPlan(ctx, planID)
}

// UpdatePlan updates an existing quota plan.
func (s *quotaAdminService) UpdatePlan(ctx context.Context, planID string, plan *admin.QuotaPlan) (*admin.QuotaPlan, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdatePlan(ctx, planID, plan)
}

// DeletePlan deletes a quota plan.
func (s *quotaAdminService) DeletePlan(ctx context.Context, planID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeletePlan(ctx, planID)
}

// SetDefaultPlan sets a quota plan as the default for new users.
func (s *quotaAdminService) SetDefaultPlan(ctx context.Context, planID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.SetDefaultPlan(ctx, planID)
}

// ListAllowances lists all quota allowances.
func (s *quotaAdminService) ListAllowances(ctx context.Context) ([]*admin.QuotaAllowance, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListAllowances(ctx)
}

// CreateAllowance creates a new quota allowance for a user.
func (s *quotaAdminService) CreateAllowance(ctx context.Context, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreateAllowance(ctx, userID, source, allowanceType, upload, download, storage, expiryDate)
}

// UpdateAllowance updates an existing quota allowance.
func (s *quotaAdminService) UpdateAllowance(ctx context.Context, grantID string, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdateAllowance(ctx, grantID, userID, source, allowanceType, upload, download, storage, expiryDate)
}

// DeleteAllowance deletes a quota allowance.
func (s *quotaAdminService) DeleteAllowance(ctx context.Context, grantID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeleteAllowance(ctx, grantID)
}

// GetStats retrieves system-wide quota statistics.
func (s *quotaAdminService) GetStats(ctx context.Context) (*admin.SystemStats, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetStats(ctx)
}

// Reconcile performs quota reconciliation for users.
func (s *quotaAdminService) Reconcile(ctx context.Context, userID *int) (string, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return "", 0, err
	}
	if s.service == nil {
		return "", 0, ErrServiceUnavailable
	}
	return s.service.Reconcile(ctx, userID)
}

// Cleanup performs quota cleanup based on retention policy.
func (s *quotaAdminService) Cleanup(ctx context.Context, retentionDays int) (int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return 0, err
	}
	if s.service == nil {
		return 0, ErrServiceUnavailable
	}
	return s.service.Cleanup(ctx, retentionDays)
}

// ListUserConfigs lists all user quota configurations with pagination.
func (s *quotaAdminService) ListUserConfigs(ctx context.Context) ([]*admin.UserQuotaConfig, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListUserConfigs(ctx)
}

// UpdateUserConfig updates a user's quota configuration.
func (s *quotaAdminService) UpdateUserConfig(ctx context.Context, userID int, config *admin.UserQuotaConfigUpdate) (*admin.UserQuotaConfig, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdateUserConfig(ctx, userID, config)
}

// ResetUserPlan removes a user's assigned quota plan (sets to NULL).
func (s *quotaAdminService) ResetUserPlan(ctx context.Context, userID int) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.ResetUserPlan(ctx, userID)
}

// RequireAuthenticated checks if the billing admin service is authenticated.
func (s *billingAdminService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}

// ListCredits lists all credits with optional filtering.
func (s *billingAdminService) ListCredits(ctx context.Context, params *admin.GetApiBillingCreditsParams) ([]*admin.CreditItem, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListCredits(ctx, params)
}

// CreateCredit creates a new credit entry.
func (s *billingAdminService) CreateCredit(ctx context.Context, req *admin.CreditCreateRequest) (*admin.Credit, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreateCredit(ctx, req)
}

// GetCredit retrieves a credit by ID.
func (s *billingAdminService) GetCredit(ctx context.Context, creditID string) (*admin.Credit, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetCredit(ctx, creditID)
}

// DeleteCredit soft deletes a credit by ID.
func (s *billingAdminService) DeleteCredit(ctx context.Context, creditID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeleteCredit(ctx, creditID)
}

// RestoreCredit restores a soft-deleted credit by ID.
func (s *billingAdminService) RestoreCredit(ctx context.Context, creditID string) (*admin.Credit, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.RestoreCredit(ctx, creditID)
}

// PurgeCredits permanently removes soft-deleted credits older than specified duration.
func (s *billingAdminService) PurgeCredits(ctx context.Context, req *admin.CreditPurgeRequest) (int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return 0, err
	}
	if s.service == nil {
		return 0, ErrServiceUnavailable
	}
	return s.service.PurgeCredits(ctx, req)
}

// GetUserBalance retrieves the current balance for a user.
func (s *billingAdminService) GetUserBalance(ctx context.Context, userID string) (*admin.UserBalance, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetUserBalance(ctx, userID)
}

// GetUserDeletedCredits retrieves soft-deleted credits for a user.
func (s *billingAdminService) GetUserDeletedCredits(ctx context.Context, userID string, params *admin.GetApiBillingUsersUserIdDeletedCreditsParams) ([]*admin.CreditItem, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.GetUserDeletedCredits(ctx, userID, params)
}

// ListPriceLines lists all price lines.
func (s *billingAdminService) ListPriceLines(ctx context.Context) ([]*admin.PriceLine, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListPriceLines(ctx)
}

// CreatePriceLine creates a new price line.
func (s *billingAdminService) CreatePriceLine(ctx context.Context, req *admin.PriceLineCreateRequest) (*admin.PriceLine, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreatePriceLine(ctx, req)
}

// GetPriceLine retrieves a price line by ID with its associated plans.
func (s *billingAdminService) GetPriceLine(ctx context.Context, priceLineID string) (*admin.PriceLineDetailResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetPriceLine(ctx, priceLineID)
}

// UpdatePriceLine updates an existing price line.
func (s *billingAdminService) UpdatePriceLine(ctx context.Context, priceLineID string, req *admin.PriceLineUpdateRequest) (*admin.PriceLine, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdatePriceLine(ctx, priceLineID, req)
}

// DeletePriceLine deletes a price line by ID.
func (s *billingAdminService) DeletePriceLine(ctx context.Context, priceLineID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeletePriceLine(ctx, priceLineID)
}

// ListPricingPlans lists all pricing plans.
func (s *billingAdminService) ListPricingPlans(ctx context.Context) ([]*admin.PricingPlanItem, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListPricingPlans(ctx)
}

// CreatePricingPlan creates a new pricing plan.
func (s *billingAdminService) CreatePricingPlan(ctx context.Context, req *admin.PricingPlanCreateRequest) (*admin.PricingPlan, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreatePricingPlan(ctx, req)
}

// UpdatePricingPlan updates an existing pricing plan.
func (s *billingAdminService) UpdatePricingPlan(ctx context.Context, planID string, req *admin.PricingPlanUpdateRequest) (*admin.PricingPlan, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdatePricingPlan(ctx, planID, req)
}

// DeletePricingPlan deletes a pricing plan by ID.
func (s *billingAdminService) DeletePricingPlan(ctx context.Context, planID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeletePricingPlan(ctx, planID)
}

// ListPricingPlanPeriods lists all pricing plan periods.
func (s *billingAdminService) ListPricingPlanPeriods(ctx context.Context) ([]*admin.PricingPlanPeriod, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListPricingPlanPeriods(ctx)
}

// CreatePricingPlanPeriod creates a new pricing plan period.
func (s *billingAdminService) CreatePricingPlanPeriod(ctx context.Context, req *admin.PricingPlanPeriodCreateRequest) (*admin.PricingPlanPeriod, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreatePricingPlanPeriod(ctx, req)
}

// GetPricingPlanPeriod retrieves a pricing plan period by ID.
func (s *billingAdminService) GetPricingPlanPeriod(ctx context.Context, periodID string) (*admin.PricingPlanPeriod, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetPricingPlanPeriod(ctx, periodID)
}

// UpdatePricingPlanPeriod updates an existing pricing plan period.
func (s *billingAdminService) UpdatePricingPlanPeriod(ctx context.Context, periodID string, req *admin.PricingPlanPeriodUpdateRequest) (*admin.PricingPlanPeriod, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdatePricingPlanPeriod(ctx, periodID, req)
}

// DeletePricingPlanPeriod deletes a pricing plan period by ID.
func (s *billingAdminService) DeletePricingPlanPeriod(ctx context.Context, periodID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeletePricingPlanPeriod(ctx, periodID)
}

// ListSubscribers lists all subscribers across all gateways.
func (s *billingAdminService) ListSubscribers(ctx context.Context) ([]*admin.Subscriber, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListSubscribers(ctx)
}

// GetSubscriber retrieves a specific subscriber by ID.
func (s *billingAdminService) GetSubscriber(ctx context.Context, subscriberID string) (*admin.Subscriber, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetSubscriber(ctx, subscriberID)
}

// ListGatewaySubscribers lists subscribers for a specific gateway.
func (s *billingAdminService) ListGatewaySubscribers(ctx context.Context, gatewayID string) ([]*admin.Subscriber, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.ListGatewaySubscribers(ctx, gatewayID)
}

// GetUserSubscribers retrieves subscribers for a specific user.
func (s *billingAdminService) GetUserSubscribers(ctx context.Context, userID string) ([]*admin.Subscriber, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	if s.service == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.service.GetUserSubscribers(ctx, userID)
}

// CancelUserSubscription cancels a user's subscription.
func (s *billingAdminService) CancelUserSubscription(ctx context.Context, userID string, req *admin.CancelSubscriptionRequest) (*admin.ManagementResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CancelUserSubscription(ctx, userID, req)
}

// AbortUserSubscriptionCancellation aborts a scheduled subscription cancellation.
func (s *billingAdminService) AbortUserSubscriptionCancellation(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.AbortUserSubscriptionCancellation(ctx, userID)
}

// ChangeUserPlan changes a user's subscription plan.
func (s *billingAdminService) ChangeUserPlan(ctx context.Context, userID string, req *admin.ChangePlanRequest) (*admin.PlanChangeResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ChangeUserPlan(ctx, userID, req)
}

// PauseUserSubscription pauses a user's subscription.
func (s *billingAdminService) PauseUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.PauseUserSubscription(ctx, userID)
}

// ResumeUserSubscription resumes a user's paused subscription.
func (s *billingAdminService) ResumeUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ResumeUserSubscription(ctx, userID)
}

// AddPlanToPriceLine adds a pricing plan to a price line.
func (s *billingAdminService) AddPlanToPriceLine(ctx context.Context, priceLineID string, req *admin.AddPlanToPriceLineRequest) (*admin.PriceLineDetailResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.AddPlanToPriceLine(ctx, priceLineID, req)
}

// DeletePlanFromPriceLine removes a pricing plan from a price line.
func (s *billingAdminService) DeletePlanFromPriceLine(ctx context.Context, priceLineID, planID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeletePlanFromPriceLine(ctx, priceLineID, planID)
}

// UpdatePlanPosition updates the position of a plan in a price line.
func (s *billingAdminService) UpdatePlanPosition(ctx context.Context, priceLineID, planID string, req *admin.UpdatePlanPositionRequest) (*admin.PriceLineDetailResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdatePlanPosition(ctx, priceLineID, planID, req)
}
