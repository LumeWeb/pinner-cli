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
//   - pinner_auth_sso: start an out-of-band sign-in; returns a needs_human
//     hand-off with the approval URL and a resume handle. Never blocks.
//   - pinner_auth_resume: poll/complete a pending out-of-band sign-in handle;
//     returns pending until the human has approved, then done.

type authSSOArgs struct {
	// Email is the account to sign in. Optional: the OOB page lets the human
	// enter/confirm the address, and resume only needs the handle.
	Email string `json:"email,omitempty"`
}

// NewAuthSSODescriptor returns the pinner_auth_sso tool: start an out-of-band
// browser login, non-blocking, returning a needs_human hand-off with the
// approval URL and a resume handle. If oob, handles, or reg is nil, it returns
// a structured "not configured" hand-off instead of hanging. It registers a
// resume continuation so the shared pinner_auth_resume template can poll the
// login to completion.
func NewAuthSSODescriptor(oob *OutOfBandLogin, handles *AsyncHandleStore, reg *HandoffRegistry) ToolDescriptor {
	return ToolDescriptor{
		Name:        "pinner_auth_sso",
		Title:       "Sign In (Out-of-Band)",
		Description: "Start an out-of-band (OOB) browser sign-in for SSO authentication. Returns immediately with an approval URL the human opens, and a resume handle for the pinner_auth_resume tool. Non-blocking, and never asks the human for a password or OTP on this channel. Start here to authenticate.",
		Category:    CategoryCore,
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

			// The handle doubles as the OOB session id so resume can
			// reconstruct the (sessionID, email) key from stored handle data.
			handle := handles.Create("pending", map[string]any{"email": in.Email})
			_, url, err := oob.Begin(handle, in.Email)
			if err != nil {
				return ToolResult{IsError: true, Text: fmt.Sprintf("failed to start out-of-band login: %v", err)}, nil
			}
			// Register the SSO-specific poll logic under the handle so the
			// shared pinner_auth_resume template dispatches to it.
			reg.Begin(handle, ssoResumeContinuation(oob, handles))
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  url,
				Handle:     handle,
				ResumeTool: "pinner_auth_resume",
				Detail:     "Ask the user to open the approval URL in their browser and complete sign-in. Then call pinner_auth_resume with the handle.",
			}), nil
		},
	}
}

// ssoResumeContinuation returns the SSO-specific poll logic: it checks whether
// the human has completed the browser approval and returns pending
// (needs_human) until done, then a terminal done result. It is registered
// against the handle by pinner_auth_sso so the shared resume template can
// dispatch to it.
func ssoResumeContinuation(oob *OutOfBandLogin, handles *AsyncHandleStore) ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (ToolResult, error) {
		email, _ := data["email"].(string)
		url, done, loginErr := oob.pendingOutcome(handle, email)
		if loginErr != nil {
			handles.Delete(handle)
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ResumeTool: "pinner_auth_sso",
				Detail:     "The out-of-band login failed or expired. Start a fresh login with pinner_auth_sso.",
			}), nil
		}
		if !done {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonSSOApproval,
				ActionURL:  url,
				Handle:     handle,
				ResumeTool: "pinner_auth_resume",
				Detail:     "Sign-in still pending; the user has not completed the approval yet.",
			}), nil
		}
		handles.Delete(handle)
		return ToolResult{
			Text:              "Sign-in complete. Authentication is now configured.",
			StructuredContent: map[string]any{"status": StatusDone, "handle": handle},
		}, nil
	}
}

// NewAuthResumeDescriptor returns the pinner_auth_resume tool, built from the
// shared resume template. The name/description, restart steering, and
// dead-handle guidance are SSO-specific; the dispatch logic (handle validation,
// expiry, continuation lookup) is shared via NewResumeTool.
func NewAuthResumeDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                "pinner_auth_resume",
		Description:         "Poll a pending out-of-band (OOB) sign in to check whether the human has completed the SSO approval (sign-in). Returns pending (needs_human) until approval is done, then reports done. Pass the handle returned by pinner_auth_sso.",
		RestartTool:         "pinner_auth_sso",
		UnknownHandleDetail: "unknown handle; start a new login with pinner_auth_sso",
		ExpiredHandleDetail: "the sign-in handle expired before the human completed approval; start a fresh login with pinner_auth_sso and have the user approve promptly",
		DeadHandleReason:    ReasonSSOApproval,
	}, reg, handles)
}
