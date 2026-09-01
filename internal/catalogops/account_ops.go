// Package catalogops implements the account domain operations for the operation
// catalog: account info, update email, update password, and subscription status
// (which also carries the web-app manage-subscription deep-link). Each operation
// drives the core auth service and returns typed data; rendering happens in the
// frontend wiring layer.
//
// These are catalog operations (not hand-written CLI subcommands) so the same
// definition compiles to BOTH the urfave CLI surface and the MCP tool surface —
// an account control authored here is reachable from `pinner account ...` and as
// an MCP tool, with identical safety/interaction/visibility metadata on each.
package catalogops

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// AccountDeps injects the dependencies the account operations need at
// construction time. All getters are resolved per invocation (lazy-deps
// pattern) so services always use fresh config, never a package-init snapshot.
type AccountDeps struct {
	// CfgMgr returns a live config manager for the current invocation. When
	// nil, operations that need config fail with a clear error.
	CfgMgr func() config.Manager
	// AuthService builds an auth.AuthService for the given config and token.
	// `token` is the per-invocation --auth-token override ("" to use the
	// config-stored credential). When nil, operations fail with a clear error.
	AuthService func(cfgMgr config.Manager, token string) auth.AuthService
	// PortalURL returns the web-app subscription page URL for the given config
	// (e.g. https://account.<portal>/account/subscription). When nil, the
	// portal/deep-link operations fail with a clear error.
	PortalURL func(cfgMgr config.Manager) string
}

// config returns the live config manager for this invocation, or nil.
func (d AccountDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// authService builds an auth.AuthService honoring the per-invocation token
// override from the input map (falling back to config), or nil when unwired.
func (d AccountDeps) authService(cfgMgr config.Manager, input map[string]any) auth.AuthService {
	if d.AuthService == nil || cfgMgr == nil {
		return nil
	}
	return d.AuthService(cfgMgr, authTokenFromInput(input))
}

// portalURL returns the web-app subscription page URL for the live config, or
// "" when the derivation is unwired or the config is unavailable.
func (d AccountDeps) portalURL() string {
	if d.PortalURL == nil {
		return ""
	}
	if cfgMgr := d.config(); cfgMgr != nil {
		return d.PortalURL(cfgMgr)
	}
	return ""
}

// authClientHandler builds a catalog Handler that resolves the live config
// manager + auth service, then invokes `fn` with the authenticated service.
// All account operations that call core through an authenticated client use
// this; it centralizes the auth-resolution boilerplate.
func authClientHandler(d AccountDeps, fn func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error)) catalog.Handler {
	return handler(func(ctx context.Context, input map[string]any) (any, error) {
		cfgMgr := d.config()
		if cfgMgr == nil {
			return nil, fmt.Errorf("account operation: no config manager available")
		}
		svc := d.authService(cfgMgr, input)
		if svc == nil {
			return nil, fmt.Errorf("account operation: auth service unavailable")
		}
		return fn(ctx, svc, input)
	})
}

// timePtrStr renders an RFC3339 string pointer from a *time.Time, or nil.
func timePtrStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// AccountOperations returns the catalog operations for the account domain.
func AccountOperations(d AccountDeps) []catalog.Operation {
	return []catalog.Operation{
		accountInfo(d),
		accountUpdateEmail(d),
		accountUpdatePassword(d),
		accountOTPDisable(d),
		accountSubscription(d),
		accountQuota(d),
	}
}

// --- Account result types (typed data returned to the frontend) ---

// AccountInfoResult is the data returned by the account_info operation.
type AccountInfoResult struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	UserID    int    `json:"user_id"`
	Verified  bool   `json:"verified"`
	OTP       bool   `json:"otp_enabled"`
}

// AccountUpdateEmailResult reports a successful email change and directs the
// user to confirm via the verification email sent to the new address.
type AccountUpdateEmailResult struct {
	Email   string `json:"email"`
	Message string `json:"message"`
}

// AccountUpdatePasswordResult reports a successful password change.
type AccountUpdatePasswordResult struct {
	Message string `json:"message"`
}

// AccountOTPDisableResult reports a successful two-factor authentication
// disable.
type AccountOTPDisableResult struct {
	Message string `json:"message"`
}

// AccountSubscriptionResult is the data returned by the account_subscription
// operation: the subscription status plus the web-portal deep-link the user
// follows to manage (or start) their subscription.
type AccountSubscriptionResult struct {
	IsSubscribed bool    `json:"is_subscribed"`
	PlanPeriodID *int    `json:"pricing_plan_period_id,omitempty"`
	GatewayType  *string `json:"gateway_type,omitempty"`
	WillCancelAt *string `json:"will_cancel_at,omitempty"`
	PausedAt     *string `json:"paused_at,omitempty"`
	CreatedAt    *string `json:"created_at,omitempty"`
	UpdatedAt    *string `json:"updated_at,omitempty"`
	Message      string  `json:"message,omitempty"`
	WebURL       string  `json:"web_url"` // deep-link to the web app subscription page
}

// accountInfo is the `account info` operation: reads the account profile.
func accountInfo(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_info",
		Title:       "Account info",
		Summary:     "Show your Pinner.xyz account profile",
		Description: "Fetch your account profile: the email address, first/last name, user id, email-verified flag, and whether 2FA is enabled.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_info to read the authenticated user's profile (email, name, user id, verified, otp_enabled). Read-only."),
		),
		Category:    "account",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			info, err := svc.GetAccount(ctx)
			if err != nil {
				return nil, fmt.Errorf("account_info: %w", err)
			}
			if info == nil {
				return &AccountInfoResult{}, nil
			}
			return &AccountInfoResult{
				Email:     info.Email,
				FirstName: info.FirstName,
				LastName:  info.LastName,
				UserID:    info.Id,
				Verified:  info.Verified,
				OTP:       info.Otp,
			}, nil
		}),
	})
}

// accountUpdateEmail is the `account email <new>` operation: changes the email,
// requiring the current password for verification.
func accountUpdateEmail(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_update_email",
		Title:       "Update account email",
		Summary:     "Change the email address on your account",
		Description: "Change the email address associated with your account. Your current password is required for verification. On success a verification email is sent to the new address.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_update_email to change the account's email address. Requires the current password for verification. On success the user must confirm via the verification email sent to the new address."),
		),
		Category:    "account",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Environment: catalog.EnvCLIOnly,
		Positional:  "<email>",
		Args: []catalog.OperationArg{
			{Name: "email", Type: catalog.ArgTypeString, Required: true, Help: "New email address", AgentHelp: "The new email address for the account."},
			{Name: "password", Type: catalog.ArgTypeString, Required: true, Sensitive: true, Help: "Current account password for verification", AgentHelp: "The user's current account password, used to verify the change."},
		},
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			email := catalog.StrArg(input, "email", "")
			password := catalog.StrArg(input, "password", "")
			if email == "" {
				return nil, fmt.Errorf("account_update_email: email is required")
			}
			if password == "" {
				return nil, fmt.Errorf("account_update_email: current password is required")
			}
			if err := svc.UpdateEmail(ctx, email, password); err != nil {
				return nil, fmt.Errorf("account_update_email: %w", err)
			}
			return &AccountUpdateEmailResult{
				Email:   email,
				Message: "Email updated. A verification email has been sent to the new address — confirm it to finalize the change.",
			}, nil
		}),
	})
}

// accountUpdatePassword is the `account password` operation: changes the
// password, requiring the current password.
func accountUpdatePassword(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_update_password",
		Title:       "Update account password",
		Summary:     "Change the password on your account",
		Description: "Change the password associated with your account. Your current password is required.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_update_password to change the account's password. Requires the current password and a new password."),
		),
		Category:    "account",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Environment: catalog.EnvCLIOnly,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "current_password", Type: catalog.ArgTypeString, Required: true, Sensitive: true, Help: "Current password", AgentHelp: "The user's current account password."},
			{Name: "new_password", Type: catalog.ArgTypeString, Required: true, Sensitive: true, Help: "New password", AgentHelp: "The new password to set for the account."},
		},
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			current := catalog.StrArg(input, "current_password", "")
			next := catalog.StrArg(input, "new_password", "")
			if current == "" {
				return nil, fmt.Errorf("account_update_password: current password is required")
			}
			if next == "" {
				return nil, fmt.Errorf("account_update_password: new password is required")
			}
			if err := svc.UpdatePassword(ctx, current, next); err != nil {
				return nil, fmt.Errorf("account_update_password: %w", err)
			}
			return &AccountUpdatePasswordResult{Message: "Password updated."}, nil
		}),
	})
}

// accountOTPDisable is the `account otp disable` operation: disables
// two-factor authentication, requiring the current account password for
// verification.
func accountOTPDisable(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_otp_disable",
		Title:       "Disable two-factor authentication",
		Summary:     "Disable 2FA on your account",
		Description: "Disables two-factor authentication on your account. Your current account password is required to verify.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_otp_disable to turn off the account's two-factor authentication. Requires the user's current account password."),
		),
		Category:    "account",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "password", Type: catalog.ArgTypeString, Required: true, Sensitive: true, Help: "Current account password for verification", AgentHelp: "The user's current account password, used to verify disabling two-factor authentication."},
		},
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			pw := catalog.StrArg(input, "password", "")
			if pw == "" {
				return nil, fmt.Errorf("account_otp_disable: password is required")
			}
			if _, err := svc.DisableOTP(ctx, pw); err != nil {
				return nil, fmt.Errorf("account_otp_disable: %w", err)
			}
			return &AccountOTPDisableResult{Message: "Two-factor authentication disabled."}, nil
		}),
	})
}

// accountSubscription is the `account subscription` operation: reads the active
// subscription status and returns the web-portal deep-link to manage it.
func accountSubscription(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_subscription",
		Title:       "Active subscription",
		Summary:     "Show your active subscription status",
		Description: "Fetch your active Pinner.xyz subscription status (subscribed, plan period, gateway, cancellation/pause state) and the web-app URL where you manage or start a subscription.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_subscription to read the user's active subscription status and obtain the web_url deep-link to https://account.<portal>/account/subscription where they sign in and manage/subscribe. The URL is returned as data; a human must open it in a browser to actually subscribe or change their plan."),
		),
		Category:    "account",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			status, err := svc.GetSubscriptionStatus(ctx)
			if err != nil {
				return nil, fmt.Errorf("account_subscription: %w", err)
			}
			out := &AccountSubscriptionResult{IsSubscribed: status != nil && status.IsSubscribed}
			if status != nil {
				out.PlanPeriodID = status.PricingPlanPeriodId
				out.GatewayType = status.GatewayType
				out.WillCancelAt = timePtrStr(status.WillCancelAt)
				out.PausedAt = timePtrStr(status.PausedAt)
				out.CreatedAt = timePtrStr(status.CreatedAt)
				out.UpdatedAt = timePtrStr(status.UpdatedAt)
			}
			out.WebURL = d.portalURL()
			if out.IsSubscribed {
				out.Message = "Subscribed. Manage your subscription in the web app."
			} else {
				out.Message = "Not subscribed. Open the web app to choose a plan and subscribe."
			}
			return out, nil
		}),
	})
}

// AccountQuotaType reports usage, limits, and remaining allowance for a single
// quota dimension (upload, download, or storage). A nil Limit means no explicit
// cap was reported; a nil Remaining means no explicit allowance was reported
// (NOT a guaranteed-unlimited signal — see remainingUsable).
type AccountQuotaType struct {
	Used       int  `json:"used"`
	Limit      *int `json:"limit,omitempty"`
	Remaining  *int `json:"remaining,omitempty"`
	Reserved   *int `json:"reserved,omitempty"`
	Threshold  *int `json:"threshold,omitempty"`
	Percentage int  `json:"percentage"`
}

// AccountQuotaResult is the data returned by the account_quota operation:
// per-dimension usage plus a derived has_quota signal. has_quota reports
// whether the account has any usable granted/remaining allowance, which TRUMPS
// subscription state: a user with quota does not need a subscription. When
// has_quota is false the caller should consult account_subscription; the
// human (not the agent) follows web_url to subscribe.
type AccountQuotaResult struct {
	Upload   AccountQuotaType `json:"upload"`
	Download AccountQuotaType `json:"download"`
	Storage  AccountQuotaType `json:"storage"`
	HasQuota bool             `json:"has_quota"`
	Message  string           `json:"message,omitempty"`
	WebURL   string           `json:"web_url"`
}

// quotaTypeFrom assembles an AccountQuotaType from a quota dimension's
// primitive fields. It takes primitives because the SDK surfaces these under
// an internal (pinner-unimportable) struct type; mapping is field-by-field.
func quotaTypeFrom(used int, limit, remaining, reserved, threshold *int, percentage int) AccountQuotaType {
	return AccountQuotaType{
		Used:       used,
		Limit:      limit,
		Remaining:  remaining,
		Reserved:   reserved,
		Threshold:  threshold,
		Percentage: percentage,
	}
}

// remainingUsable reports whether a remaining-allowance bound proves usable.
// A nil remaining means the server omitted an allowance bound for this
// dimension; the QuotaStatusResponse schema marks only used/percentage as
// required and carries no explicit unlimited marker, so a nil remaining must
// NOT count as covered — otherwise accounts with no quota grant (all nil) are
// reported as has_quota=true. Coverage requires an explicit positive remaining.
func remainingUsable(remaining *int) bool {
	return remaining != nil && *remaining > 0
}

// accountQuota is the `account quota` operation: reads the account's quota
// status and derives whether granted usage covers access (quota trumps a
// subscription).
func accountQuota(d AccountDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "account_quota",
		Title:       "Account quota",
		Summary:     "Show your quota usage and remaining allowance",
		Description: "Fetch your Pinner.xyz quota status (upload, download, and storage usage/limits/remaining) and whether granted usage currently covers the account. Quota TRUMPS a subscription: if has_quota is true the user can proceed without a subscription. Returns the web_app web_url deep-link for managing usage / subscribing.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call account_quota to read the account's quota status and whether it is covered by granted usage (has_quota). Quota trumps a subscription: if has_quota is true the user needs no subscription and you should proceed. Only when has_quota is false, fall back to account_subscription; if that says not-subscribed, present the returned web_url deep-link so the human opens the web app to subscribe — you cannot subscribe on their behalf."),
		),
		Category:    "account",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: authClientHandler(d, func(ctx context.Context, svc auth.AuthService, input map[string]any) (any, error) {
			quota, err := svc.GetQuota(ctx)
			if err != nil {
				return nil, fmt.Errorf("account_quota: %w", err)
			}
			out := &AccountQuotaResult{}
			if quota != nil {
				out.Upload = quotaTypeFrom(quota.Upload.Used, quota.Upload.Limit, quota.Upload.Remaining, quota.Upload.Reserved, quota.Upload.Threshold, quota.Upload.Percentage)
				out.Download = quotaTypeFrom(quota.Download.Used, quota.Download.Limit, quota.Download.Remaining, quota.Download.Reserved, quota.Download.Threshold, quota.Download.Percentage)
				out.Storage = quotaTypeFrom(quota.Storage.Used, quota.Storage.Limit, quota.Storage.Remaining, quota.Storage.Reserved, quota.Storage.Threshold, quota.Storage.Percentage)
				out.HasQuota = remainingUsable(quota.Upload.Remaining) || remainingUsable(quota.Download.Remaining) || remainingUsable(quota.Storage.Remaining)
			}
			out.WebURL = d.portalURL()
			out.WebURL = d.portalURL()
			if out.HasQuota {
				out.Message = "Account is covered by granted quota; no subscription required."
			} else {
				out.Message = "No usable quota remaining. A subscription (or additional granted usage) is needed to continue."
			}
			return out, nil
		}),
	})
}
