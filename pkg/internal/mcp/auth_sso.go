package mcp

import (
	"context"
	"errors"
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

type authResumeArgs struct {
	// Handle is the handle returned by pinner_auth_sso.
	Handle string `json:"handle,omitempty"`
}

// NewAuthSSODescriptor returns the pinner_auth_sso tool: start an out-of-band
// browser login, non-blocking, returning a needs_human hand-off with the
// approval URL and a resume handle. If oob or handles is nil, it returns a
// structured "not configured" hand-off instead of hanging.
func NewAuthSSODescriptor(oob *OutOfBandLogin, handles *AsyncHandleStore) ToolDescriptor {
	return ToolDescriptor{
		Name:        "pinner_auth_sso",
		Title:       "Sign In (Out-of-Band)",
		Description: "Start an out-of-band browser sign-in. Returns immediately with an approval URL the human opens, and a resume handle for pinner_auth_resume. Password/OTP never transit this channel. Non-blocking.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[authSSOArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if oob == nil || handles == nil {
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

// NewAuthResumeDescriptor returns the pinner_auth_resume tool: poll/complete a
// pending out-of-band sign-in handle.
func NewAuthResumeDescriptor(oob *OutOfBandLogin, handles *AsyncHandleStore) ToolDescriptor {
	return ToolDescriptor{
		Name:        "pinner_auth_resume",
		Title:       "Sign In Resume",
		Description: "Poll a pending out-of-band sign-in. Returns pending (needs_human) until the human completes the approval, then done. Pass the handle from pinner_auth_sso.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[authResumeArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if oob == nil || handles == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Out-of-band login is not configured for this server.",
				}), nil
			}
			in, err := decodeToolArgs[authResumeArgs](req)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}
			if in.Handle == "" {
				return ToolResult{IsError: true, Text: "handle is required"}, nil
			}
			_, data, err := handles.Get(in.Handle)
			if err != nil {
				detail := "unknown handle; start a new login with pinner_auth_sso"
				if errors.Is(err, ErrHandleExpired) {
					detail = "the sign-in handle expired before the human completed approval; start a fresh login with pinner_auth_sso and have the user approve promptly"
				}
				// A login that can no longer be resumed must not leave the agent
				// retrying a dead handle. Steer it to start over immediately.
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonSSOApproval,
					ResumeTool: "pinner_auth_sso",
					Detail:     detail,
				}), nil
			}
			email, _ := data["email"].(string)

			url, done, loginErr := oob.pendingOutcome(in.Handle, email)
			if loginErr != nil {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonSSOApproval,
					Handle:     in.Handle,
					ResumeTool: "pinner_auth_sso",
					Detail:     "The out-of-band login failed or expired. Start a fresh login with pinner_auth_sso.",
				}), nil
			}
			if !done {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonSSOApproval,
					ActionURL:  url,
					Handle:     in.Handle,
					ResumeTool: "pinner_auth_resume",
					Detail:     "Sign-in still pending; the user has not completed the approval yet.",
				}), nil
			}
			handles.Delete(in.Handle)
			return ToolResult{
				Text:              "Sign-in complete. Authentication is now configured.",
				StructuredContent: map[string]any{"status": StatusDone, "handle": in.Handle},
			}, nil
		},
	}
}
