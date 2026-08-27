package auth

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
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
// authSSODescription is shared between the static Description and the Fallback
// MCPTarget so the descriptor carries a target list.
const authSSODescription = "Start an out-of-band (OOB) browser sign-in for SSO authentication. Returns immediately with an approval URL the human opens, and a resume handle for the auth_resume tool. Non-blocking, and never asks the human for a password or OTP on this channel. Start here to authenticate."

func NewAuthSSODescriptor(oob *OutOfBandLogin, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "auth_sso",
		Title:       "Sign In (Out-of-Band)",
		Description: authSSODescription,
		Category:    model.CategoryAccount,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(authSSODescription)),
		InputSchema: toolargs.ToolSchemaFor[authSSOArgs](),
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			if oob == nil || handles == nil || reg == nil {
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason: model.ReasonInteractiveOnly,
					Detail: "Out-of-band login is not configured for this server. Use the CLI 'pinner auth' to sign in, or run with the transport that provides a browser login.",
				}), nil
			}
			in, err := toolargs.DecodeToolArgs[authSSOArgs](req)
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}

			// Single-flight: begin the out-of-band login atomically, or resume
			// an already-in-flight one. Two concurrent browser sign-ins
			// deadlock: a human can only complete one approval, so whichever
			// side is polling the other login never resolves. BeginOrResume
			// runs the pending-login existence check and (if none) the insert
			// under one lock, so simultaneous triggers (the Sign In GUI and the
			// model) converge on the SAME handle rather than minting competing
			// logins. The returned id is the resume handle AND the approval-link
			// token (a single identifier removes the "which id do I resume with"
			// ambiguity), and reused reports whether an existing login was
			// surfaced.
			id, url, reused, err := oob.BeginOrResume(in.Email)
			if err != nil {
				return model.ToolResult{IsError: true, Text: fmt.Sprintf("failed to start out-of-band login: %v", err)}, nil
			}
			// Ensure the async handle exists under this id and register the
			// SSO-specific poll continuation so auth_resume / auth_sso_status
			// dispatch to it. Both are idempotent for the reused (in-use) case.
			handles.CreateWithID(id, "pending", map[string]any{"email": in.Email})
			reg.Begin(id, SSOResumeContinuation(oob, handles, reg))

			if reused {
				// An existing login is in flight: surface the SAME in-use handle
				// and approval URL, flagged in_use with an explicit revoke path
				// (auth_sso_revoke) so the caller can start fresh if desired.
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason:     model.ReasonSSOApproval,
					ActionURL:  url,
					Handle:     id,
					ResumeTool: "auth_resume",
					InUse:      true,
					RevokeTool: "auth_sso_revoke",
					Detail:     "A sign-in is already in progress (handle " + id + "). Complete that pending approval, or revoke it first with auth_sso_revoke to start a fresh sign-in.",
				}), nil
			}
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonSSOApproval,
				ActionURL:  url,
				Handle:     id,
				ResumeTool: "auth_resume",
				Detail:     "Ask the user to open the approval URL in their browser and complete sign-in. Then call auth_resume with the handle.",
			}), nil
		},
	}
}

// SSOResumeContinuation returns the SSO-specific poll logic: it checks whether
// the human has completed the browser approval and returns pending
// (needs_human) until done, then a terminal done result. It is registered
// against the handle by auth_sso so the shared resume template can
// dispatch to it.
//
// The continuation performs its own registry cleanup (reg.End) on every
// terminal outcome — done, login error, or already-consumed/no-pending-request
// — rather than relying solely on the template's isTerminalResume. The
// concurrent double-resume path in PendingOutcome returns
// ("", false, nil) when another resume already consumed the request; that is a
// completed outcome from the human's perspective, so it is reported done, not
// misleadingly "still pending".
func SSOResumeContinuation(oob *OutOfBandLogin, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry) handoff.ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (model.ToolResult, error) {
		email, _ := data["email"].(string)
		url, done, loginErr := oob.PendingOutcome(handle, email)
		if loginErr != nil {
			handles.Delete(handle)
			reg.End(handle)
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonSSOApproval,
				ResumeTool: "auth_sso",
				Detail:     "The out-of-band login failed or expired. Start a fresh login with auth_sso.",
			}), nil
		}
		if !done {
			// PendingOutcome returns ("", false, nil) when there is no pending
			// request for this handle — either the login was already consumed
			// by a concurrent resume or it never registered. Either way the
			// flow has concluded from the OOB side; report done rather than a
			// misleading "still pending" with no URL to approve.
			if url == "" {
				handles.Delete(handle)
				reg.End(handle)
				return model.ToolResult{
					Text:              "Sign-in complete. Authentication is now configured.",
					StructuredContent: map[string]any{"status": model.StatusDone, "handle": handle},
				}, nil
			}
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonSSOApproval,
				ActionURL:  url,
				Handle:     handle,
				ResumeTool: "auth_resume",
				Detail:     "Sign-in still pending; the user has not completed the approval yet.",
			}), nil
		}
		handles.Delete(handle)
		reg.End(handle)
		return model.ToolResult{
			Text:              "Sign-in complete. Authentication is now configured.",
			StructuredContent: map[string]any{"status": model.StatusDone, "handle": handle},
		}, nil
	}
}

// authSSORevokeArgs is the argument for auth_sso_revoke: the handle of the
// in-flight sign-in to cancel.
type authSSORevokeArgs struct {
	Handle string `json:"handle"`
}

// revokeSSOLogin cancels an in-flight sign-in by handle across its three
// backing stores: the OOB login request, the async handle, and the resume
// continuation. It is the single teardown both the model-facing auth_sso_revoke
// and any future app helper share, so revocation always retires all three. It
// reports whether an OOB login request was actually pending and revoked.
func revokeSSOLogin(oob *OutOfBandLogin, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry, handle string) bool {
	revoked := oob != nil && oob.Revoke(handle)
	if handles != nil {
		handles.Delete(handle)
	}
	if reg != nil {
		reg.End(handle)
	}
	return revoked
}

// NewAuthSSORevokeDescriptor returns the auth_sso_revoke tool: cancel an
// in-flight out-of-band sign-in by handle so a fresh auth_sso can start. It
// pairs with the single-flight behavior of auth_sso: when auth_sso reports a
// handle is already in use, the caller can complete that pending approval or
// revoke it here. Revoking retires the OOB login request (its approval URL
// becomes a "no longer active" page), the resume handle, and its continuation.
func NewAuthSSORevokeDescriptor(oob *OutOfBandLogin, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "auth_sso_revoke",
		Title:       "Revoke In-Progress Sign-In",
		Description: "Cancel an in-progress out-of-band sign-in by handle, so a fresh auth_sso can start. Use this when auth_sso reports a handle is already in use (in_use=true) but you or the human want to start a new sign-in instead of completing the pending approval.",
		Category:    model.CategoryAccount,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback("Cancel an in-progress out-of-band sign-in by handle, so a fresh auth_sso can start. Use when auth_sso reports a handle is already in use.")),
		InputSchema: toolargs.ToolSchemaFor[authSSORevokeArgs](),
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			if oob == nil || handles == nil || reg == nil {
				return model.ToolResult{IsError: true, Text: "Revoking a sign-in is not configured for this server."}, nil
			}
			in, err := toolargs.DecodeToolArgs[authSSORevokeArgs](req)
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}
			if in.Handle == "" {
				return model.ToolResult{IsError: true, Text: "handle is required"}, nil
			}
			revoked := revokeSSOLogin(oob, handles, reg, in.Handle)
			return model.ToolResult{
				Text: "Sign-in revoked. Start a fresh login with auth_sso when ready.",
				StructuredContent: map[string]any{
					"status":  model.StatusDone,
					"handle":  in.Handle,
					"revoked": revoked,
				},
			}, nil
		},
	}
}

// NewAuthResumeDescriptor returns the auth_resume tool, built from the
// shared resume template. The name/description, restart steering, and
// dead-handle guidance are SSO-specific; the dispatch logic (handle validation,
// expiry, continuation lookup) is shared via handoff.NewResumeTool.
func NewAuthResumeDescriptor(reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return handoff.NewResumeTool(handoff.ResumeToolSpec{
		Name:                "auth_resume",
		Title:               "Auth Sign-In Resume",
		Description:         "Poll a pending out-of-band (OOB) sign in to check whether the human has completed the SSO approval (sign-in). Returns pending (needs_human) until approval is done, then reports done. Pass the handle returned by auth_sso.",
		RestartTool:         "auth_sso",
		UnknownHandleDetail: "unknown handle; start a new login with auth_sso",
		ExpiredHandleDetail: "the sign-in handle expired before the human completed approval; start a fresh login with auth_sso and have the user approve promptly",
		DeadHandleReason:    model.ReasonSSOApproval,
		Category:            model.CategoryAccount,
	}, reg, handles)
}
