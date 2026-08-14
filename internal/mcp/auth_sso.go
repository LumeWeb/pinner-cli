package mcp

import (
	"context"
	"fmt"
)

// This file exposes the out-of-band (browser) login coordinator as first-class
// agent tools. The OOB login completes sign-in in a browser so credentials
// never transit the MCP/LLM channel. These tools make that flow non-blocking
// and resumable for agents (and humans collaborating with agents):
//
//   - auth_sso: start an out-of-band sign-in; returns a needs_human
//     hand-off with the approval URL and a resume handle. Never blocks.
//   - auth_resume: poll/complete a pending out-of-band sign-in handle;
//     returns pending until the human has approved, then done.

type authSSOArgs struct {
	// Email is the account to sign in. Optional: the OOB page lets the human
	// enter/confirm the address, and resume only needs the handle.
	Email string `json:"email,omitempty"`
}

// NewAuthSSODescriptor returns the auth_sso tool: start an out-of-band
// browser login, non-blocking, returning a needs_human hand-off with the
// approval URL and a resume handle. If oob, handles, or reg is nil, it returns
// a structured "not configured" hand-off instead of hanging. It registers a
// resume continuation so the shared auth_resume template can poll the
// login to completion.
func NewAuthSSODescriptor(oob *OutOfBandLogin, handles *AsyncHandleStore, reg *HandoffRegistry) ToolDescriptor {
	return ToolDescriptor{
		Name:        "auth_sso",
		Title:       "Sign In (Out-of-Band)",
		Description: "Start an out-of-band (OOB) browser sign-in for SSO authentication. Returns immediately with an approval URL the human opens, and a resume handle for the auth_resume tool. Non-blocking, and never asks the human for a password or OTP on this channel. Start here to authenticate.",
		Category:    CategoryAccount,
		InputSchema: toolSchemaFor[authSSOArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if oob == nil || handles == nil || reg == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Out-of-band login is not configured for this server. Use the CLI 'pinner auth' to sign in, or run with the transport that provides a browser login.",
				}), nil
			}
			in, err := decodeToolArgs[authSSOArgs](req)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}

			// The handle doubles as the OOB session id AND the request id so
			// every client-facing identifier for this login is the same value:
			// the resume handle, the approval-link token, and what auth_resume
			// validates against. A single identifier per login removes the
			// "which id do I resume with" ambiguity that previously caused
			// auth_resume to report "unknown handle" when an agent used the
			// approval-link token (which was a different, request-only id).
			handle := handles.Create("pending", map[string]any{"email": in.Email})
			_, url, err := oob.BeginWithID(handle, handle, in.Email)
			if err != nil {
				// Do not leave an orphaned handle in the store with no
				// continuation registered; retire it so a future resume does
				// not see a live handle with nothing backing it.
				handles.Delete(handle)
				return ToolResult{IsError: true, Text: fmt.Sprintf("failed to start out-of-band login: %v", err)}, nil
			}
			// Register the SSO-specific poll logic under the handle so the
			// shared auth_resume template dispatches to it.
			reg.Begin(handle, ssoResumeContinuation(oob, handles, reg))
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  url,
				Handle:     handle,
				ResumeTool: "auth_resume",
				Detail:     "Ask the user to open the approval URL in their browser and complete sign-in. Then call auth_resume with the handle.",
			}), nil
		},
	}
}

// ssoResumeContinuation returns the SSO-specific poll logic: it checks whether
// the human has completed the browser approval and returns pending
// (needs_human) until done, then a terminal done result. It is registered
// against the handle by auth_sso so the shared resume template can
// dispatch to it.
//
// The continuation performs its own registry cleanup (reg.End) on every
// terminal outcome — done, login error, or already-consumed/no-pending-request
// — rather than relying solely on the template's isTerminalResume. The
// concurrent double-resume path in pendingOutcome returns
// ("", false, nil) when another resume already consumed the request; that is a
// completed outcome from the human's perspective, so it is reported done, not
// misleadingly "still pending".
func ssoResumeContinuation(oob *OutOfBandLogin, handles *AsyncHandleStore, reg *HandoffRegistry) ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (ToolResult, error) {
		email, _ := data["email"].(string)
		url, done, loginErr := oob.pendingOutcome(handle, email)
		if loginErr != nil {
			handles.Delete(handle)
			reg.End(handle)
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ResumeTool: "auth_sso",
				Detail:     "The out-of-band login failed or expired. Start a fresh login with auth_sso.",
			}), nil
		}
		if !done {
			// pendingOutcome returns ("", false, nil) when there is no pending
			// request for this handle — either the login was already consumed
			// by a concurrent resume or it never registered. Either way the
			// flow has concluded from the OOB side; report done rather than a
			// misleading "still pending" with no URL to approve.
			if url == "" {
				handles.Delete(handle)
				reg.End(handle)
				return ToolResult{
					Text:              "Sign-in complete. Authentication is now configured.",
					StructuredContent: map[string]any{"status": StatusDone, "handle": handle},
				}, nil
			}
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  url,
				Handle:     handle,
				ResumeTool: "auth_resume",
				Detail:     "Sign-in still pending; the user has not completed the approval yet.",
			}), nil
		}
		handles.Delete(handle)
		reg.End(handle)
		return ToolResult{
			Text:              "Sign-in complete. Authentication is now configured.",
			StructuredContent: map[string]any{"status": StatusDone, "handle": handle},
		}, nil
	}
}

// NewAuthResumeDescriptor returns the auth_resume tool, built from the
// shared resume template. The name/description, restart steering, and
// dead-handle guidance are SSO-specific; the dispatch logic (handle validation,
// expiry, continuation lookup) is shared via NewResumeTool.
func NewAuthResumeDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                "auth_resume",
		Title:               "Auth Sign-In Resume",
		Description:         "Poll a pending out-of-band (OOB) sign in to check whether the human has completed the SSO approval (sign-in). Returns pending (needs_human) until approval is done, then reports done. Pass the handle returned by auth_sso.",
		RestartTool:         "auth_sso",
		UnknownHandleDetail: "unknown handle; start a new login with auth_sso",
		ExpiredHandleDetail: "the sign-in handle expired before the human completed approval; start a fresh login with auth_sso and have the user approve promptly",
		DeadHandleReason:    ReasonSSOApproval,
		Category:            CategoryAccount,
	}, reg, handles)
}
