package mcp

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// This file exposes the out-of-band account credential tools: changing the
// password (OOB browser form) and requesting a password reset (deep-link to
// the webapp). In both cases the password never transits the MCP/LLM channel:
//
//   - account_password_update: mints a one-time /account/password/<token> page
//     the human opens in a browser to enter their current + new password. It
//     enforces that the account is signed in first (steering to auth_sso
//     otherwise), because UpdatePassword requires an authenticated session.
//   - account_password_reset: starts the email reset flow via the portal's
//     unauthenticated RequestPasswordReset, deep-linking to the webapp where
//     the human completes it via the emailed link. Requires no password here.

// accountPasswordUpdateArgs is the input of account_password_update. No
// password fields: the human enters them on the browser page.
type accountPasswordUpdateArgs struct {
	// Email is optional context; the page derives nothing from it. It is NOT
	// the password.
	Email string `json:"email,omitempty"`
}

// accountPasswordResetArgs is the input of account_password_reset.
type accountPasswordResetArgs struct {
	// Email is the account address to send the reset link to. Required.
	Email string `json:"email,omitempty"`
}

// NewAccountPasswordUpdateDescriptor returns the account_password_update tool:
// mint an out-of-band password-change page for the authenticated account. It
// returns a needs_human hand-off with the page URL; the human completes the
// change in a browser so the password never reaches this channel. If the
// account is not signed in (or the coordinator is unwired) it steers to
// auth_sso instead of hanging.
func NewAccountPasswordUpdateDescriptor(oob *OOBAccountChange, svc AuthService, handles *AsyncHandleStore, reg *HandoffRegistry) ToolDescriptor {
	return ToolDescriptor{
		Name:        "account_password_update",
		Title:       "Change Password (Out-of-Band)",
		Description: "Start an out-of-band (OOB) password change. Returns a short-lived page URL the human opens in a browser to enter their current and new password; the password never reaches this channel. The account must be signed in first. Start here to change your password.",
		Category:    CategoryAccount,
		Destructive: true,
		InputSchema: toolSchemaFor[accountPasswordUpdateArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if oob == nil || svc == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Out-of-band password change is not configured for this server. Use the CLI (pinner account update-password) to change your password.",
				}), nil
			}
			in, err := decodeToolArgs[accountPasswordUpdateArgs](req)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}
			_ = in

			// Enforce an authenticated session: UpdatePassword requires one.
			// steer to auth_sso rather than returning an unusable page.
			if _, err := svc.Status(ctx); err != nil {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonSSOApproval,
					ResumeTool: "auth_sso",
					Detail:     "You must be signed in to change your password. Please sign in first (auth_sso), then run account_password_update again.",
				}), nil
			}

			url := oob.Register(opChangePassword)
			if url == "" {
				return ToolResult{IsError: true, Text: "failed to mint the password-change page"}, nil
			}
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  url,
				Detail:     "Ask the user to open this URL in a browser and enter their current and new password. The password is entered on that page, never on this channel, and never here.",
			}), nil
		},
	}
}

// NewAccountEmailChangeDescriptor returns the account_email_change tool: mint
// an out-of-band email-change page for the authenticated account. It returns a
// needs_human hand-off with the page URL; the human enters the new email +
// current password in a browser so the password never reaches this channel. If
// the account is not signed in (or the coordinator is unwired) it steers to
// auth_sso instead of hanging.
func NewAccountEmailChangeDescriptor(oob *OOBAccountChange, svc AuthService) ToolDescriptor {
	return ToolDescriptor{
		Name:        "account_email_change",
		Title:       "Change Email (Out-of-Band)",
		Description: "Start an out-of-band (OOB) email change. Returns a short-lived page URL the human opens in a browser to enter the new email and their current password; the password never reaches this channel. The account must be signed in first. Start here to change your email.",
		Category:    CategoryAccount,
		Destructive: true,
		InputSchema: toolSchemaFor[accountPasswordUpdateArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if oob == nil || svc == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Out-of-band email change is not configured for this server. Use the web app to change your email.",
				}), nil
			}
			if _, err := decodeToolArgs[accountPasswordUpdateArgs](req); err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}

			// Enforce an authenticated session: UpdateEmail requires one.
			if _, err := svc.Status(ctx); err != nil {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonSSOApproval,
					ResumeTool: "auth_sso",
					Detail:     "You must be signed in to change your email. Please sign in first (auth_sso), then run account_email_change again.",
				}), nil
			}

			url := oob.Register(opChangeEmail)
			if url == "" {
				return ToolResult{IsError: true, Text: "failed to mint the email-change page"}, nil
			}
			return NeedsHumanResult(NeedsHuman{
				Reason:    ReasonSSOApproval,
				ActionURL: url,
				Detail:    "Ask the user to open this URL in a browser and enter their new email and current password. The password is entered on that page, never on this channel.",
			}), nil
		},
	}
}

// NewAccountPasswordResetDescriptor returns the account_password_reset tool:
// start the portal's email password-reset flow. It calls RequestPasswordReset
// (unauthenticated) and returns a needs_human hand-off telling the human to
// check their email and follow the reset link to the webapp. No password
// transits this channel. When the service is unwired it returns a structured
// not-configured hand-off. webAppURL is the account web app base URL (e.g.
// https://account.<portal>) surfaced as the reset landing page.
func NewAccountPasswordResetDescriptor(svc AuthService, webAppURL string) ToolDescriptor {
	return ToolDescriptor{
		Name:        "account_password_reset",
		Title:       "Reset Password (Email Link)",
		Description: "Send a password reset link to the account's email and hand the human off to the webapp to complete it. Used when the password is forgotten; no password transits this channel.",
		Category:    CategoryAccount,
		InputSchema: toolSchemaFor[accountPasswordResetArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if svc == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Password reset is not configured for this server. Use the web app to reset your password.",
				}), nil
			}
			in, err := decodeToolArgs[accountPasswordResetArgs](req)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}
			if in.Email == "" {
				return ToolResult{IsError: true, Text: "email is required"}, nil
			}
			if err := svc.RequestPasswordReset(ctx, in.Email); err != nil {
				return ToolResult{IsError: true, Text: fmt.Sprintf("failed to request password reset: %v", err)}, nil
			}
			actionURL := webAppURL
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  actionURL,
				Detail:     "Password reset link sent to " + in.Email + ". Ask the user to check their email and open the reset link to set a new password in the web app.",
			}), nil
		},
	}
}

// accountWebAppURL returns the account web app base URL (e.g.
// https://account.<portal>) surfaced by the password-reset hand-off, or "" when
// the config manager is unavailable.
func accountWebAppURL(cfgMgr config.Manager) string {
	if cfgMgr == nil {
		return ""
	}
	return cfgMgr.Config().GetAccountEndpointSecure()
}
