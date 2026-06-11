package cli

import (
	"context"
	"sync"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// adminServiceBase contains fields and methods common to all admin services.
type adminServiceBase struct {
	tokenProvider *AdminTokenProvider
	endpoint      string
	authenticated bool
	mu            sync.RWMutex
}

// RequireAuthenticated checks if the admin service is authenticated.
func (b *adminServiceBase) RequireAuthenticated() error {
	if !b.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}

// quotaAdminService implements the QuotaAdminService interface using the admin.QuotaService.
type quotaAdminService struct {
	*adminServiceBase
	service *admin.QuotaService
}

// billingAdminService implements the BillingAdminService interface using the admin.BillingService.
type billingAdminService struct {
	*adminServiceBase
	service *admin.BillingService
}

// websiteAdminService implements the WebsiteAdminService interface using the admin.WebsiteService.
type websiteAdminService struct {
	*adminServiceBase
	service *admin.WebsiteService
}

// profilingAdminService implements the ProfilingAdminService interface using the admin.ProfilingService.
type profilingAdminService struct {
	*adminServiceBase
	service *admin.ProfilingService
}

// QuotaAdminServiceFactory creates a QuotaAdminService with dependencies.
type QuotaAdminServiceFactory func(cfgMgr config.Manager, output Output) QuotaAdminService

// BillingAdminServiceFactory creates a BillingAdminService with dependencies.
type BillingAdminServiceFactory func(cfgMgr config.Manager, output Output) BillingAdminService

// WebsiteAdminServiceFactory creates a WebsiteAdminService with dependencies.
type WebsiteAdminServiceFactory func(cfgMgr config.Manager, output Output) WebsiteAdminService

// ProfilingAdminServiceFactory creates a ProfilingAdminService with dependencies.
type ProfilingAdminServiceFactory func(cfgMgr config.Manager, output Output) ProfilingAdminService

// defaultQuotaAdminServiceFactory creates a default QuotaAdminService instance.
func defaultQuotaAdminServiceFactory(cfgMgr config.Manager, output Output) QuotaAdminService {
	return NewQuotaAdminService(cfgMgr, output, cfgMgr.Config().GetAdminEndpoint())
}

// defaultBillingAdminServiceFactory creates a default BillingAdminService instance.
func defaultBillingAdminServiceFactory(cfgMgr config.Manager, output Output) BillingAdminService {
	return NewBillingAdminService(cfgMgr, output, cfgMgr.Config().GetAdminEndpoint())
}

// defaultWebsiteAdminServiceFactory creates a default WebsiteAdminService instance.
func defaultWebsiteAdminServiceFactory(cfgMgr config.Manager, output Output) WebsiteAdminService {
	return NewWebsiteAdminService(cfgMgr, output, cfgMgr.Config().GetAdminEndpoint())
}

// defaultProfilingAdminServiceFactory creates a default ProfilingAdminService instance.
func defaultProfilingAdminServiceFactory(cfgMgr config.Manager, output Output) ProfilingAdminService {
	return NewProfilingAdminService(cfgMgr, output, cfgMgr.Config().GetAdminEndpoint())
}

// newAdminServiceBase creates a new adminServiceBase with the shared fields.
func newAdminServiceBase(cfgMgr config.Manager, endpoint string) *adminServiceBase {
	authToken := cfgMgr.Config().AuthToken
	return &adminServiceBase{
		tokenProvider: NewAdminTokenProvider(cfgMgr),
		endpoint:      endpoint,
		authenticated: authToken != "",
	}
}

type authedService[S any] interface {
	RequireAuthenticated() error
	getService(ctx context.Context) (S, error)
}

func with2[S any, T any](svc authedService[S], ctx context.Context, fn func(S) (T, error)) (T, error) {
	var zero T
	if err := svc.RequireAuthenticated(); err != nil {
		return zero, err
	}
	s, err := svc.getService(ctx)
	if err != nil {
		return zero, err
	}
	return fn(s)
}

func with3[S any, T any](svc authedService[S], ctx context.Context, fn func(S) ([]T, int, error)) ([]T, int, error) {
	if err := svc.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}
	s, err := svc.getService(ctx)
	if err != nil {
		return nil, 0, err
	}
	return fn(s)
}

func with0[S any](svc authedService[S], ctx context.Context, fn func(S) error) error {
	if err := svc.RequireAuthenticated(); err != nil {
		return err
	}
	s, err := svc.getService(ctx)
	if err != nil {
		return err
	}
	return fn(s)
}

func with2i[S any](svc authedService[S], ctx context.Context, fn func(S) (int, error)) (int, error) {
	if err := svc.RequireAuthenticated(); err != nil {
		return 0, err
	}
	s, err := svc.getService(ctx)
	if err != nil {
		return 0, err
	}
	return fn(s)
}

// NewQuotaAdminService creates a new QuotaAdminService instance.
func NewQuotaAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) QuotaAdminService {
	return &quotaAdminService{
		adminServiceBase: newAdminServiceBase(cfgMgr, apiEndpoint),
	}
}

// NewBillingAdminService creates a new BillingAdminService instance.
func NewBillingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) BillingAdminService {
	return &billingAdminService{
		adminServiceBase: newAdminServiceBase(cfgMgr, apiEndpoint),
	}
}

// NewWebsiteAdminService creates a new WebsiteAdminService instance.
func NewWebsiteAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) WebsiteAdminService {
	return &websiteAdminService{
		adminServiceBase: newAdminServiceBase(cfgMgr, apiEndpoint),
	}
}

// NewProfilingAdminService creates a new ProfilingAdminService instance.
func NewProfilingAdminService(cfgMgr config.Manager, output Output, apiEndpoint string) ProfilingAdminService {
	return &profilingAdminService{
		adminServiceBase: newAdminServiceBase(cfgMgr, apiEndpoint),
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
	GetPricingPlan(ctx context.Context, planID string) (*admin.PricingPlan, error)
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

	// Sync operations
	SyncPricingPlan(ctx context.Context, planID string) error
	SyncAllPricingPlans(ctx context.Context) error
}

// WebsiteAdminService defines the interface for website admin operations.
type WebsiteAdminService interface {
	RequireAuthenticated() error
	BlockWebsite(ctx context.Context, id string) (*admin.Website, error)
	UnblockWebsite(ctx context.Context, id string) (*admin.Website, error)
}

// ProfilingAdminService defines the interface for profiling admin operations.
type ProfilingAdminService interface {
	RequireAuthenticated() error
	GetProfileIndex(ctx context.Context) ([]byte, error)
	GetBlockProfile(ctx context.Context) ([]byte, error)
	SetBlockProfileRate(ctx context.Context, rate int) error
	GetCmdline(ctx context.Context) ([]byte, error)
	GetGoroutineProfile(ctx context.Context) ([]byte, error)
	GetHeapProfile(ctx context.Context) ([]byte, error)
	GetMutexProfile(ctx context.Context) ([]byte, error)
	SetMutexProfileFraction(ctx context.Context, fraction int) error
	GetCPUProfile(ctx context.Context) ([]byte, error)
	GetStatus(ctx context.Context) (*admin.ProfilingStatus, error)
	GetSymbol(ctx context.Context) ([]byte, error)
	GetThreadcreate(ctx context.Context) ([]byte, error)
	GetTrace(ctx context.Context) ([]byte, error)
}

// getService returns the quota service, lazily initializing with token exchange if needed.
func (s *quotaAdminService) getService(ctx context.Context) (*admin.QuotaService, error) {
	s.mu.RLock()
	if s.service != nil {
		s.mu.RUnlock()
		return s.service, nil
	}
	s.mu.RUnlock()

	token, err := s.tokenProvider.GetLoginToken(ctx)
	if err != nil {
		return nil, err
	}

	client := admin.NewClient(
		admin.WithEndpoint(s.endpoint),
		admin.WithJWT(token),
	)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client.Quota()
	return s.service, nil
}

// ListPlans lists all quota plans.
func (s *quotaAdminService) ListPlans(ctx context.Context) ([]*admin.QuotaPlan, int, error) {
	return with3(s, ctx, func(svc *admin.QuotaService) ([]*admin.QuotaPlan, int, error) {
		return svc.ListPlans(ctx)
	})
}

// CreatePlan creates a new quota plan.
func (s *quotaAdminService) CreatePlan(ctx context.Context, plan *admin.QuotaPlan) (*admin.QuotaPlan, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.QuotaPlan, error) {
		return svc.CreatePlan(ctx, plan)
	})
}

// GetPlan retrieves a quota plan by ID.
func (s *quotaAdminService) GetPlan(ctx context.Context, planID string) (*admin.QuotaPlan, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.QuotaPlan, error) {
		return svc.GetPlan(ctx, planID)
	})
}

// UpdatePlan updates an existing quota plan.
func (s *quotaAdminService) UpdatePlan(ctx context.Context, planID string, plan *admin.QuotaPlan) (*admin.QuotaPlan, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.QuotaPlan, error) {
		return svc.UpdatePlan(ctx, planID, plan)
	})
}

// DeletePlan deletes a quota plan.
func (s *quotaAdminService) DeletePlan(ctx context.Context, planID string) error {
	return with0(s, ctx, func(svc *admin.QuotaService) error {
		return svc.DeletePlan(ctx, planID)
	})
}

// SetDefaultPlan sets a quota plan as the default for new users.
func (s *quotaAdminService) SetDefaultPlan(ctx context.Context, planID string) error {
	return with0(s, ctx, func(svc *admin.QuotaService) error {
		return svc.SetDefaultPlan(ctx, planID)
	})
}

// ListAllowances lists all quota allowances.
func (s *quotaAdminService) ListAllowances(ctx context.Context) ([]*admin.QuotaAllowance, int, error) {
	return with3(s, ctx, func(svc *admin.QuotaService) ([]*admin.QuotaAllowance, int, error) {
		return svc.ListAllowances(ctx)
	})
}

// CreateAllowance creates a new quota allowance for a user.
func (s *quotaAdminService) CreateAllowance(ctx context.Context, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.QuotaAllowance, error) {
		return svc.CreateAllowance(ctx, userID, source, allowanceType, upload, download, storage, expiryDate)
	})
}

// UpdateAllowance updates an existing quota allowance.
func (s *quotaAdminService) UpdateAllowance(ctx context.Context, grantID string, userID int, source, allowanceType string, upload, download, storage int, expiryDate time.Time) (*admin.QuotaAllowance, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.QuotaAllowance, error) {
		return svc.UpdateAllowance(ctx, grantID, userID, source, allowanceType, upload, download, storage, expiryDate)
	})
}

// DeleteAllowance deletes a quota allowance.
func (s *quotaAdminService) DeleteAllowance(ctx context.Context, grantID string) error {
	return with0(s, ctx, func(svc *admin.QuotaService) error {
		return svc.DeleteAllowance(ctx, grantID)
	})
}

// GetStats retrieves system-wide quota statistics.
func (s *quotaAdminService) GetStats(ctx context.Context) (*admin.SystemStats, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.SystemStats, error) {
		return svc.GetStats(ctx)
	})
}

// Reconcile performs quota reconciliation for users.
// NOTE: Reconcile returns (string, int, error) which doesn't fit with0/with2/with3/with2i.
func (s *quotaAdminService) Reconcile(ctx context.Context, userID *int) (string, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return "", 0, err
	}
	svc, err := s.getService(ctx)
	if err != nil {
		return "", 0, err
	}
	return svc.Reconcile(ctx, userID)
}

// Cleanup performs quota cleanup based on retention policy.
func (s *quotaAdminService) Cleanup(ctx context.Context, retentionDays int) (int, error) {
	return with2i(s, ctx, func(svc *admin.QuotaService) (int, error) {
		return svc.Cleanup(ctx, retentionDays)
	})
}

// ListUserConfigs lists all user quota configurations with pagination.
func (s *quotaAdminService) ListUserConfigs(ctx context.Context) ([]*admin.UserQuotaConfig, int, error) {
	return with3(s, ctx, func(svc *admin.QuotaService) ([]*admin.UserQuotaConfig, int, error) {
		return svc.ListUserConfigs(ctx)
	})
}

// UpdateUserConfig updates a user's quota configuration.
func (s *quotaAdminService) UpdateUserConfig(ctx context.Context, userID int, config *admin.UserQuotaConfigUpdate) (*admin.UserQuotaConfig, error) {
	return with2(s, ctx, func(svc *admin.QuotaService) (*admin.UserQuotaConfig, error) {
		return svc.UpdateUserConfig(ctx, userID, config)
	})
}

// ResetUserPlan removes a user's assigned quota plan (sets to NULL).
func (s *quotaAdminService) ResetUserPlan(ctx context.Context, userID int) error {
	return with0(s, ctx, func(svc *admin.QuotaService) error {
		return svc.ResetUserPlan(ctx, userID)
	})
}

// getService returns the billing service, lazily initializing with token exchange if needed.
func (s *billingAdminService) getService(ctx context.Context) (*admin.BillingService, error) {
	s.mu.RLock()
	if s.service != nil {
		s.mu.RUnlock()
		return s.service, nil
	}
	s.mu.RUnlock()

	token, err := s.tokenProvider.GetLoginToken(ctx)
	if err != nil {
		return nil, err
	}

	client := admin.NewClient(
		admin.WithEndpoint(s.endpoint),
		admin.WithJWT(token),
	)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client.Billing()
	return s.service, nil
}

// ListCredits lists all credits with optional filtering.
func (s *billingAdminService) ListCredits(ctx context.Context, params *admin.GetApiBillingCreditsParams) ([]*admin.CreditItem, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.CreditItem, int, error) {
		return svc.ListCredits(ctx, params)
	})
}

// CreateCredit creates a new credit entry.
func (s *billingAdminService) CreateCredit(ctx context.Context, req *admin.CreditCreateRequest) (*admin.Credit, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.Credit, error) {
		return svc.CreateCredit(ctx, req)
	})
}

// GetCredit retrieves a credit by ID.
func (s *billingAdminService) GetCredit(ctx context.Context, creditID string) (*admin.Credit, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.Credit, error) {
		return svc.GetCredit(ctx, creditID)
	})
}

// DeleteCredit soft deletes a credit by ID.
func (s *billingAdminService) DeleteCredit(ctx context.Context, creditID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.DeleteCredit(ctx, creditID)
	})
}

// RestoreCredit restores a soft-deleted credit by ID.
func (s *billingAdminService) RestoreCredit(ctx context.Context, creditID string) (*admin.Credit, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.Credit, error) {
		return svc.RestoreCredit(ctx, creditID)
	})
}

// PurgeCredits permanently removes soft-deleted credits older than specified duration.
func (s *billingAdminService) PurgeCredits(ctx context.Context, req *admin.CreditPurgeRequest) (int, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (int, error) {
		return svc.PurgeCredits(ctx, req)
	})
}

// GetUserBalance retrieves the current balance for a user.
func (s *billingAdminService) GetUserBalance(ctx context.Context, userID string) (*admin.UserBalance, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.UserBalance, error) {
		return svc.GetUserBalance(ctx, userID)
	})
}

// GetUserDeletedCredits retrieves soft-deleted credits for a user.
func (s *billingAdminService) GetUserDeletedCredits(ctx context.Context, userID string, params *admin.GetApiBillingUsersUserIdDeletedCreditsParams) ([]*admin.CreditItem, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.CreditItem, int, error) {
		return svc.GetUserDeletedCredits(ctx, userID, params)
	})
}

// ListPriceLines lists all price lines.
func (s *billingAdminService) ListPriceLines(ctx context.Context) ([]*admin.PriceLine, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.PriceLine, int, error) {
		return svc.ListPriceLines(ctx)
	})
}

// CreatePriceLine creates a new price line.
func (s *billingAdminService) CreatePriceLine(ctx context.Context, req *admin.PriceLineCreateRequest) (*admin.PriceLine, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PriceLine, error) {
		return svc.CreatePriceLine(ctx, req)
	})
}

// GetPriceLine retrieves a price line by ID with its associated plans.
func (s *billingAdminService) GetPriceLine(ctx context.Context, priceLineID string) (*admin.PriceLineDetailResponse, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PriceLineDetailResponse, error) {
		return svc.GetPriceLine(ctx, priceLineID)
	})
}

// UpdatePriceLine updates an existing price line.
func (s *billingAdminService) UpdatePriceLine(ctx context.Context, priceLineID string, req *admin.PriceLineUpdateRequest) (*admin.PriceLine, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PriceLine, error) {
		return svc.UpdatePriceLine(ctx, priceLineID, req)
	})
}

// DeletePriceLine deletes a price line by ID.
func (s *billingAdminService) DeletePriceLine(ctx context.Context, priceLineID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.DeletePriceLine(ctx, priceLineID)
	})
}

// ListPricingPlans lists all pricing plans.
func (s *billingAdminService) ListPricingPlans(ctx context.Context) ([]*admin.PricingPlanItem, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.PricingPlanItem, int, error) {
		return svc.ListPricingPlans(ctx)
	})
}

// GetPricingPlan retrieves a pricing plan by ID.
func (s *billingAdminService) GetPricingPlan(ctx context.Context, planID string) (*admin.PricingPlan, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlan, error) {
		return svc.GetPricingPlan(ctx, planID)
	})
}

// CreatePricingPlan creates a new pricing plan.
func (s *billingAdminService) CreatePricingPlan(ctx context.Context, req *admin.PricingPlanCreateRequest) (*admin.PricingPlan, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlan, error) {
		return svc.CreatePricingPlan(ctx, req)
	})
}

// UpdatePricingPlan updates an existing pricing plan.
func (s *billingAdminService) UpdatePricingPlan(ctx context.Context, planID string, req *admin.PricingPlanUpdateRequest) (*admin.PricingPlan, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlan, error) {
		return svc.UpdatePricingPlan(ctx, planID, req)
	})
}

// DeletePricingPlan deletes a pricing plan by ID.
func (s *billingAdminService) DeletePricingPlan(ctx context.Context, planID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.DeletePricingPlan(ctx, planID)
	})
}

// ListPricingPlanPeriods lists all pricing plan periods.
func (s *billingAdminService) ListPricingPlanPeriods(ctx context.Context) ([]*admin.PricingPlanPeriod, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.PricingPlanPeriod, int, error) {
		return svc.ListPricingPlanPeriods(ctx)
	})
}

// CreatePricingPlanPeriod creates a new pricing plan period.
func (s *billingAdminService) CreatePricingPlanPeriod(ctx context.Context, req *admin.PricingPlanPeriodCreateRequest) (*admin.PricingPlanPeriod, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlanPeriod, error) {
		return svc.CreatePricingPlanPeriod(ctx, req)
	})
}

// GetPricingPlanPeriod retrieves a pricing plan period by ID.
func (s *billingAdminService) GetPricingPlanPeriod(ctx context.Context, periodID string) (*admin.PricingPlanPeriod, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlanPeriod, error) {
		return svc.GetPricingPlanPeriod(ctx, periodID)
	})
}

// UpdatePricingPlanPeriod updates an existing pricing plan period.
func (s *billingAdminService) UpdatePricingPlanPeriod(ctx context.Context, periodID string, req *admin.PricingPlanPeriodUpdateRequest) (*admin.PricingPlanPeriod, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PricingPlanPeriod, error) {
		return svc.UpdatePricingPlanPeriod(ctx, periodID, req)
	})
}

// DeletePricingPlanPeriod deletes a pricing plan period by ID.
func (s *billingAdminService) DeletePricingPlanPeriod(ctx context.Context, periodID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.DeletePricingPlanPeriod(ctx, periodID)
	})
}

// ListSubscribers lists all subscribers across all gateways.
func (s *billingAdminService) ListSubscribers(ctx context.Context) ([]*admin.Subscriber, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.Subscriber, int, error) {
		return svc.ListSubscribers(ctx)
	})
}

// GetSubscriber retrieves a specific subscriber by ID.
func (s *billingAdminService) GetSubscriber(ctx context.Context, subscriberID string) (*admin.Subscriber, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.Subscriber, error) {
		return svc.GetSubscriber(ctx, subscriberID)
	})
}

// ListGatewaySubscribers lists subscribers for a specific gateway.
func (s *billingAdminService) ListGatewaySubscribers(ctx context.Context, gatewayID string) ([]*admin.Subscriber, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.Subscriber, int, error) {
		return svc.ListGatewaySubscribers(ctx, gatewayID)
	})
}

// GetUserSubscribers retrieves subscribers for a specific user.
func (s *billingAdminService) GetUserSubscribers(ctx context.Context, userID string) ([]*admin.Subscriber, int, error) {
	return with3(s, ctx, func(svc *admin.BillingService) ([]*admin.Subscriber, int, error) {
		return svc.GetUserSubscribers(ctx, userID)
	})
}

// CancelUserSubscription cancels a user's subscription.
func (s *billingAdminService) CancelUserSubscription(ctx context.Context, userID string, req *admin.CancelSubscriptionRequest) (*admin.ManagementResult, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.ManagementResult, error) {
		return svc.CancelUserSubscription(ctx, userID, req)
	})
}

// AbortUserSubscriptionCancellation aborts a scheduled subscription cancellation.
func (s *billingAdminService) AbortUserSubscriptionCancellation(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.ManagementResult, error) {
		return svc.AbortUserSubscriptionCancellation(ctx, userID)
	})
}

// ChangeUserPlan changes a user's subscription plan.
func (s *billingAdminService) ChangeUserPlan(ctx context.Context, userID string, req *admin.ChangePlanRequest) (*admin.PlanChangeResult, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PlanChangeResult, error) {
		return svc.ChangeUserPlan(ctx, userID, req)
	})
}

// PauseUserSubscription pauses a user's subscription.
func (s *billingAdminService) PauseUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.ManagementResult, error) {
		return svc.PauseUserSubscription(ctx, userID)
	})
}

// ResumeUserSubscription resumes a user's paused subscription.
func (s *billingAdminService) ResumeUserSubscription(ctx context.Context, userID string) (*admin.ManagementResult, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.ManagementResult, error) {
		return svc.ResumeUserSubscription(ctx, userID)
	})
}

// AddPlanToPriceLine adds a pricing plan to a price line.
func (s *billingAdminService) AddPlanToPriceLine(ctx context.Context, priceLineID string, req *admin.AddPlanToPriceLineRequest) (*admin.PriceLineDetailResponse, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PriceLineDetailResponse, error) {
		return svc.AddPlanToPriceLine(ctx, priceLineID, req)
	})
}

// DeletePlanFromPriceLine removes a pricing plan from a price line.
func (s *billingAdminService) DeletePlanFromPriceLine(ctx context.Context, priceLineID, planID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.DeletePlanFromPriceLine(ctx, priceLineID, planID)
	})
}

// UpdatePlanPosition updates the position of a plan in a price line.
func (s *billingAdminService) UpdatePlanPosition(ctx context.Context, priceLineID, planID string, req *admin.UpdatePlanPositionRequest) (*admin.PriceLineDetailResponse, error) {
	return with2(s, ctx, func(svc *admin.BillingService) (*admin.PriceLineDetailResponse, error) {
		return svc.UpdatePlanPosition(ctx, priceLineID, planID, req)
	})
}

// SyncPricingPlan triggers sync of a specific pricing plan with payment gateway.
func (s *billingAdminService) SyncPricingPlan(ctx context.Context, planID string) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.SyncPricingPlan(ctx, planID)
	})
}

// SyncAllPricingPlans triggers sync of all pricing plans with payment gateways.
func (s *billingAdminService) SyncAllPricingPlans(ctx context.Context) error {
	return with0(s, ctx, func(svc *admin.BillingService) error {
		return svc.SyncAllPricingPlans(ctx)
	})
}

// getService returns the website service, lazily initializing with token exchange if needed.
func (s *websiteAdminService) getService(ctx context.Context) (*admin.WebsiteService, error) {
	s.mu.RLock()
	if s.service != nil {
		s.mu.RUnlock()
		return s.service, nil
	}
	s.mu.RUnlock()

	token, err := s.tokenProvider.GetLoginToken(ctx)
	if err != nil {
		return nil, err
	}

	client := admin.NewClient(
		admin.WithEndpoint(s.endpoint),
		admin.WithJWT(token),
	)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client.Website()
	return s.service, nil
}

// BlockWebsite blocks a website by its ID.
func (s *websiteAdminService) BlockWebsite(ctx context.Context, id string) (*admin.Website, error) {
	return with2(s, ctx, func(svc *admin.WebsiteService) (*admin.Website, error) {
		return svc.BlockWebsite(ctx, id)
	})
}

// UnblockWebsite unblocks a website by its ID.
func (s *websiteAdminService) UnblockWebsite(ctx context.Context, id string) (*admin.Website, error) {
	return with2(s, ctx, func(svc *admin.WebsiteService) (*admin.Website, error) {
		return svc.UnblockWebsite(ctx, id)
	})
}

// getService returns the profiling service, lazily initializing with token exchange if needed.
func (s *profilingAdminService) getService(ctx context.Context) (*admin.ProfilingService, error) {
	s.mu.RLock()
	if s.service != nil {
		s.mu.RUnlock()
		return s.service, nil
	}
	s.mu.RUnlock()

	token, err := s.tokenProvider.GetLoginToken(ctx)
	if err != nil {
		return nil, err
	}

	client := admin.NewClient(
		admin.WithEndpoint(s.endpoint),
		admin.WithJWT(token),
	)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client.Profiling()
	return s.service, nil
}

func (s *profilingAdminService) GetProfileIndex(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetProfileIndex(ctx)
	})
}

func (s *profilingAdminService) GetBlockProfile(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetBlockProfile(ctx)
	})
}

func (s *profilingAdminService) SetBlockProfileRate(ctx context.Context, rate int) error {
	return with0(s, ctx, func(svc *admin.ProfilingService) error {
		return svc.SetBlockProfileRate(ctx, rate)
	})
}

func (s *profilingAdminService) GetCmdline(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetCmdline(ctx)
	})
}

func (s *profilingAdminService) GetGoroutineProfile(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetGoroutineProfile(ctx)
	})
}

func (s *profilingAdminService) GetHeapProfile(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetHeapProfile(ctx)
	})
}

func (s *profilingAdminService) GetMutexProfile(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetMutexProfile(ctx)
	})
}

func (s *profilingAdminService) SetMutexProfileFraction(ctx context.Context, fraction int) error {
	return with0(s, ctx, func(svc *admin.ProfilingService) error {
		return svc.SetMutexProfileFraction(ctx, fraction)
	})
}

func (s *profilingAdminService) GetCPUProfile(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetCPUProfile(ctx)
	})
}

func (s *profilingAdminService) GetStatus(ctx context.Context) (*admin.ProfilingStatus, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) (*admin.ProfilingStatus, error) {
		return svc.GetStatus(ctx)
	})
}

func (s *profilingAdminService) GetSymbol(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetSymbol(ctx)
	})
}

func (s *profilingAdminService) GetThreadcreate(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetThreadcreate(ctx)
	})
}

func (s *profilingAdminService) GetTrace(ctx context.Context) ([]byte, error) {
	return with2(s, ctx, func(svc *admin.ProfilingService) ([]byte, error) {
		return svc.GetTrace(ctx)
	})
}
