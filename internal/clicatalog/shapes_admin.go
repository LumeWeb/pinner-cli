package clicatalog

// shapes_admin.go declares the declarative command shape for the admin catalog
// domain, consumed by CompileCommandTree.
//
// Admin is the largest and most idiosyncratic domain. Its ops are grouped into
// five *section* parents under the `admin` root — quota, billing, websites,
// platform-domains, social-providers. Each section carries its own Usage and
// Category, and two sections (quota, billing) further nest *group* parents
// (plans, allowances, user-configs / credits, price-lines, pricing-plans,
// pricing-plan-periods, subscribers) that the earlier hand-written wiring
// expressed as prefix string tables ({plans_, CmdPlans}, ...). Those tables and
// the section/group parents are declared here as DomainRoot.Sections + per-op
// Segments — no string-prefix inference remains in the mount.
//
// Canonical op names (all-underscore, from catalogops.AdminOperations in stable
// declaration order) map as:
//
//	admin_platform_domains_list         -> {"admin","platform-domains","list"}
//	admin_websites_block                -> {"admin","websites","block"}
//	admin_social_providers_list         -> {"admin","social-providers","list"}
//	admin_quota_plans_set_default       -> {"admin","quota","plans","set-default"}
//	admin_quota_stats                   -> {"admin","quota","stats"}
//	admin_billing_credits_list          -> {"admin","billing","credits","list"}
//	admin_billing_overview              -> {"admin","billing","overview"}
//	... etc.
//
// Every op lives under exactly one section (no flat leaves escape the root),
// so ExcludeFromFlatMount and FoldFlat are unused here. Group-parent names use
// their display hyphenation (plans, allowances, user-configs, credits,
// price-lines, pricing-plans, pricing-plan-periods, subscribers); the flat
// leaves hyphenate their canonical underscore token exactly as the mount
// `hyphenate` helper did (set_default -> set-default, list_gateway ->
// list-gateway, ...). Order is left at 0 so tied siblings keep the
// registry-declaration order (which matches AdminOperations' emission order and
// the registration test), per the model's deterministic (Order, declaration)
// sort.

// AdminShapes is the ShapeRegistry for the admin domain, keyed by the
// all-underscore canonical op Name.
var AdminShapes = ShapeRegistry{
	// --- admin platform-domains (flat section leaves) ---
	"admin_platform_domains_list":     {Path: []string{"admin", "platform-domains", "list"}},
	"admin_platform_domains_register": {Path: []string{"admin", "platform-domains", "register"}},
	"admin_platform_domains_update":   {Path: []string{"admin", "platform-domains", "update"}},
	"admin_platform_domains_delete":   {Path: []string{"admin", "platform-domains", "delete"}},
	"admin_platform_domains_bind":     {Path: []string{"admin", "platform-domains", "bind"}},

	// --- admin websites (flat section leaves) ---
	"admin_websites_block":   {Path: []string{"admin", "websites", "block"}},
	"admin_websites_unblock": {Path: []string{"admin", "websites", "unblock"}},

	// --- admin social-providers (flat section leaves) ---
	"admin_social_providers_list":    {Path: []string{"admin", "social-providers", "list"}},
	"admin_social_providers_get":     {Path: []string{"admin", "social-providers", "get"}},
	"admin_social_providers_create":  {Path: []string{"admin", "social-providers", "create"}},
	"admin_social_providers_update":  {Path: []string{"admin", "social-providers", "update"}},
	"admin_social_providers_delete":  {Path: []string{"admin", "social-providers", "delete"}},
	"admin_social_providers_enable":  {Path: []string{"admin", "social-providers", "enable"}},
	"admin_social_providers_disable": {Path: []string{"admin", "social-providers", "disable"}},

	// --- admin quota: plans group ---
	"admin_quota_plans_list": {
		Path:     []string{"admin", "quota", "plans", "list"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},
	"admin_quota_plans_get": {
		Path:     []string{"admin", "quota", "plans", "get"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},
	"admin_quota_plans_create": {
		Path:     []string{"admin", "quota", "plans", "create"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},
	"admin_quota_plans_update": {
		Path:     []string{"admin", "quota", "plans", "update"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},
	"admin_quota_plans_delete": {
		Path:     []string{"admin", "quota", "plans", "delete"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},
	"admin_quota_plans_set_default": {
		Path:     []string{"admin", "quota", "plans", "set-default"},
		Segments: map[int]Segment{2: quotaPlansSegment},
	},

	// --- admin quota: allowances group ---
	"admin_quota_allowances_list": {
		Path:     []string{"admin", "quota", "allowances", "list"},
		Segments: map[int]Segment{2: quotaAllowancesSegment},
	},
	"admin_quota_allowances_create": {
		Path:     []string{"admin", "quota", "allowances", "create"},
		Segments: map[int]Segment{2: quotaAllowancesSegment},
	},
	"admin_quota_allowances_update": {
		Path:     []string{"admin", "quota", "allowances", "update"},
		Segments: map[int]Segment{2: quotaAllowancesSegment},
	},
	"admin_quota_allowances_delete": {
		Path:     []string{"admin", "quota", "allowances", "delete"},
		Segments: map[int]Segment{2: quotaAllowancesSegment},
	},

	// --- admin quota: user-configs group ---
	"admin_quota_user_configs_list": {
		Path:     []string{"admin", "quota", "user-configs", "list"},
		Segments: map[int]Segment{2: quotaUserConfigsSegment},
	},
	"admin_quota_user_configs_update": {
		Path:     []string{"admin", "quota", "user-configs", "update"},
		Segments: map[int]Segment{2: quotaUserConfigsSegment},
	},
	"admin_quota_user_configs_reset": {
		Path:     []string{"admin", "quota", "user-configs", "reset"},
		Segments: map[int]Segment{2: quotaUserConfigsSegment},
	},

	// --- admin quota: flat section leaves ---
	"admin_quota_stats":     {Path: []string{"admin", "quota", "stats"}},
	"admin_quota_reconcile": {Path: []string{"admin", "quota", "reconcile"}},
	"admin_quota_cleanup":   {Path: []string{"admin", "quota", "cleanup"}},

	// --- admin billing: credits group ---
	"admin_billing_credits_list": {
		Path:     []string{"admin", "billing", "credits", "list"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_get": {
		Path:     []string{"admin", "billing", "credits", "get"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_create": {
		Path:     []string{"admin", "billing", "credits", "create"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_delete": {
		Path:     []string{"admin", "billing", "credits", "delete"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_restore": {
		Path:     []string{"admin", "billing", "credits", "restore"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_purge": {
		Path:     []string{"admin", "billing", "credits", "purge"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_user_balance": {
		Path:     []string{"admin", "billing", "credits", "user-balance"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},
	"admin_billing_credits_user_deleted_credits": {
		Path:     []string{"admin", "billing", "credits", "user-deleted-credits"},
		Segments: map[int]Segment{2: billingCreditsSegment},
	},

	// --- admin billing: price-lines group ---
	"admin_billing_price_lines_list": {
		Path:     []string{"admin", "billing", "price-lines", "list"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_get": {
		Path:     []string{"admin", "billing", "price-lines", "get"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_create": {
		Path:     []string{"admin", "billing", "price-lines", "create"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_update": {
		Path:     []string{"admin", "billing", "price-lines", "update"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_delete": {
		Path:     []string{"admin", "billing", "price-lines", "delete"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_add_plan": {
		Path:     []string{"admin", "billing", "price-lines", "add-plan"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_delete_plan": {
		Path:     []string{"admin", "billing", "price-lines", "delete-plan"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},
	"admin_billing_price_lines_update_plan_position": {
		Path:     []string{"admin", "billing", "price-lines", "update-plan-position"},
		Segments: map[int]Segment{2: billingPriceLinesSegment},
	},

	// --- admin billing: pricing-plans group ---
	"admin_billing_pricing_plans_list": {
		Path:     []string{"admin", "billing", "pricing-plans", "list"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_get": {
		Path:     []string{"admin", "billing", "pricing-plans", "get"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_create": {
		Path:     []string{"admin", "billing", "pricing-plans", "create"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_update": {
		Path:     []string{"admin", "billing", "pricing-plans", "update"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_delete": {
		Path:     []string{"admin", "billing", "pricing-plans", "delete"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_sync": {
		Path:     []string{"admin", "billing", "pricing-plans", "sync"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},
	"admin_billing_pricing_plans_sync_all": {
		Path:     []string{"admin", "billing", "pricing-plans", "sync-all"},
		Segments: map[int]Segment{2: billingPricingPlansSegment},
	},

	// --- admin billing: pricing-plan-periods group ---
	"admin_billing_pricing_plan_periods_list": {
		Path:     []string{"admin", "billing", "pricing-plan-periods", "list"},
		Segments: map[int]Segment{2: billingPricingPlanPeriodsSegment},
	},
	"admin_billing_pricing_plan_periods_get": {
		Path:     []string{"admin", "billing", "pricing-plan-periods", "get"},
		Segments: map[int]Segment{2: billingPricingPlanPeriodsSegment},
	},
	"admin_billing_pricing_plan_periods_create": {
		Path:     []string{"admin", "billing", "pricing-plan-periods", "create"},
		Segments: map[int]Segment{2: billingPricingPlanPeriodsSegment},
	},
	"admin_billing_pricing_plan_periods_update": {
		Path:     []string{"admin", "billing", "pricing-plan-periods", "update"},
		Segments: map[int]Segment{2: billingPricingPlanPeriodsSegment},
	},
	"admin_billing_pricing_plan_periods_delete": {
		Path:     []string{"admin", "billing", "pricing-plan-periods", "delete"},
		Segments: map[int]Segment{2: billingPricingPlanPeriodsSegment},
	},

	// --- admin billing: subscribers group ---
	"admin_billing_subscribers_list": {
		Path:     []string{"admin", "billing", "subscribers", "list"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_get": {
		Path:     []string{"admin", "billing", "subscribers", "get"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_list_gateway": {
		Path:     []string{"admin", "billing", "subscribers", "list-gateway"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_list_user": {
		Path:     []string{"admin", "billing", "subscribers", "list-user"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_cancel": {
		Path:     []string{"admin", "billing", "subscribers", "cancel"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_abort_cancel": {
		Path:     []string{"admin", "billing", "subscribers", "abort-cancel"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_change_plan": {
		Path:     []string{"admin", "billing", "subscribers", "change-plan"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_pause": {
		Path:     []string{"admin", "billing", "subscribers", "pause"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},
	"admin_billing_subscribers_resume": {
		Path:     []string{"admin", "billing", "subscribers", "resume"},
		Segments: map[int]Segment{2: billingSubscribersSegment},
	},

	// --- admin billing: flat section leaf ---
	"admin_billing_overview": {Path: []string{"admin", "billing", "overview"}},
}

// The quota group parents under the admin `quota` section. Usage strings
// reproduce the previous `"Manage " + group + " (admin)"` parents.
var (
	quotaPlansSegment = Segment{
		Name: "plans", Category: "Admin", Usage: "Manage plans (admin)",
	}
	quotaAllowancesSegment = Segment{
		Name: "allowances", Category: "Admin", Usage: "Manage allowances (admin)",
	}
	quotaUserConfigsSegment = Segment{
		Name: "user-configs", Category: "Admin", Usage: "Manage user-configs (admin)",
	}
)

// The billing group parents under the admin `billing` section.
var (
	billingCreditsSegment = Segment{
		Name: "credits", Category: "Admin", Usage: "Manage credits (admin)",
	}
	billingPriceLinesSegment = Segment{
		Name: "price-lines", Category: "Admin", Usage: "Manage price-lines (admin)",
	}
	billingPricingPlansSegment = Segment{
		Name: "pricing-plans", Category: "Admin", Usage: "Manage pricing-plans (admin)",
	}
	billingPricingPlanPeriodsSegment = Segment{
		Name: "pricing-plan-periods", Category: "Admin", Usage: "Manage pricing-plan-periods (admin)",
	}
	billingSubscribersSegment = Segment{
		Name: "subscribers", Category: "Admin", Usage: "Manage subscribers (admin)",
	}
)

// AdminDomainRoot is the DomainRoot declaration for the admin domain. The root
// command itself is constructed by the consuming CLI mount (newAdminCommand);
// CompileCommandTree returns its ordered section children (quota, billing,
// websites, platform-domains, social-providers). The hand-written `pprof`
// section (admin_pprof.go) is not catalog-backed and is merged in by the mount.
// Each section's Category is "Admin" (matching the mounted sections) and carries
// its historical Usage line.
var AdminDomainRoot = DomainRoot{
	Name:     "admin",
	Category: "Admin",
	Usage:    "Administrative operations",
	Desc: `Administrative operations for quota management, billing, and profiling.

These commands require administrative privileges and are intended for system administrators.

Quota operations include:
  - List, create, update, delete quota plans
  - Manage user quota allowances
  - View system statistics
  - Reconcile quotas and cleanup expired data
  - Manage user quota configurations

Billing operations include:
  - Manage billing credits
  - View user balances
  - Manage price lines and pricing plans
  - Manage subscribers and subscriptions

Profiling operations include:
  - Access Go runtime pprof profiles (heap, cpu, goroutine, etc.)
  - Configure block and mutex profiling rates
  - View profiling status

Social provider operations include:
  - List, create, update, delete social login providers
  - Enable/disable providers for login

Examples:
  pinner admin quota plans list
  pinner admin quota allowances list
  pinner admin billing credits list
  pinner admin billing subscribers list
  pinner admin pprof status
  pinner admin pprof heap > heap.prof`,
	Sections: []Segment{
		{Name: "quota", Category: "Admin", Usage: "Quota management operations"},
		{Name: "billing", Category: "Admin", Usage: "Billing management operations"},
		{Name: "websites", Category: "Admin", Usage: "Manage IPFS websites (admin)"},
		{Name: "platform-domains", Category: "Admin", Usage: "Manage platform (free-subdomain) root domains"},
		{Name: "social-providers", Category: "Admin", Usage: "Manage social login providers"},
	},
}
