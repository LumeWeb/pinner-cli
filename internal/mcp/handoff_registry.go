package mcp

import (
	"context"
	"errors"
	"sync"
)

// This file provides the GENERIC, internally-shared machinery behind the
// out-of-band hand-off (start → needs_human → resume → done) protocol.
//
// The mechanics are DRY here ONCE and reused by every hand-off flow (SSO
// login, vault seed create, vault seed restore, future OTP/device hand-offs):
//   - a start-point op mints a handle and registers a per-handle continuation,
//   - the matching named *_resume tool dispatches on that handle.
//
// The SURFACE stays per-domain: each flow exposes its own named *_resume tool
// (e.g. pinner_auth_resume, future pinner_vault_resume) so an LLM can
// pattern-match the flow from the tool name. Only the internals are shared via
// NewResumeTool.

// ResumeContinuation completes or polls a pending hand-off for a handle. It is
// domain-specific (SSO polls its OOB login; vault polls its seed/restore
// coordinator), registered against the handle when the flow starts. It returns
// either a NeedsHumanResult (hand-off still pending — the caller should keep
// polling) or a terminal done result (completed).
type ResumeContinuation func(ctx context.Context, handle string, data map[string]any) (ToolResult, error)

// HandoffRegistry is the shared internal registry of per-handle resume
// continuations. A start-point tool calls Begin(handle, cont) before returning
// its needs_human hand-off; the template resume tool looks the handle up by
// handle to dispatch. One registry instance is shared by every hand-off flow
// on the server.
type HandoffRegistry struct {
	mu   sync.RWMutex
	cont map[string]ResumeContinuation
}

// NewHandoffRegistry returns an empty hand-off continuation registry.
func NewHandoffRegistry() *HandoffRegistry {
	return &HandoffRegistry{cont: make(map[string]ResumeContinuation)}
}

// Begin registers the continuation for a handle. It overwrites any prior
// continuation for the same handle. Call this when a hand-off flow starts,
// BEFORE returning the needs_human hand-off with that handle.
func (r *HandoffRegistry) Begin(handle string, c ResumeContinuation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cont[handle] = c
}

// Get returns the continuation registered for a handle, if any.
func (r *HandoffRegistry) Get(handle string) (ResumeContinuation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cont[handle]
	return c, ok
}

// End removes the continuation for a handle. Call it once the hand-off reaches
// a terminal state so the entry cannot be resumed again.
func (r *HandoffRegistry) End(handle string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cont, handle)
}

// resumeArgs is the input of any *_resume tool.
type resumeArgs struct {
	// Handle is the handle returned by the matching start tool.
	Handle string `json:"handle,omitempty"`
}

// ResumeToolSpec carries the per-domain copy + restart steering for a *_resume
// tool built from the shared template. The dispatch LOGIC is shared (see
// NewResumeTool); the description and the dead-handle guidance text are
// domain-flavored so an agent sees the right restart instruction for the flow
// it is in (SSO says "start a fresh login", vault says "start a fresh setup").
type ResumeToolSpec struct {
	// Name is the tool name (e.g. "pinner_auth_resume").
	Name string
	// Description is the agent-facing description.
	Description string
	// RestartTool is the start tool to steer to when the handle is dead.
	RestartTool string
	// UnknownHandleDetail is shown for a handle that never existed.
	UnknownHandleDetail string
	// ExpiredHandleDetail is shown for a handle past its TTL.
	ExpiredHandleDetail string
	// DeadHandleReason is the HandoffReason used when steering the agent to
	// restart after a dead handle. Domain-specific so the steer reads naturally
	// for the flow (SSO uses ReasonSSOApproval).
	DeadHandleReason HandoffReason
}

// deadHandleReason returns the domain's HandoffReason for steering an agent
// away from a dead/unresumable handle, defaulting to ReasonCredentialEntry.
func (s ResumeToolSpec) deadHandleReason() HandoffReason {
	if s.DeadHandleReason != "" {
		return s.DeadHandleReason
	}
	return ReasonCredentialEntry
}

// NewResumeTool builds a per-domain, NAMED *_resume tool from the shared
// template. All of the resume dispatch logic is written here ONCE:
//
//   - decode + require the handle,
//   - AsyncHandleStore Get for TTL/expiry (a missing/expired handle steers the
//     agent to restart the flow instead of retrying a dead handle),
//   - HandoffRegistry lookup to dispatch to the domain's ResumeContinuation,
//   - a continuation returning pending (needs_human) vs terminal (done).
//
// A domain supplies its tool spec (name, description, restart steering, and
// dead-handle guidance text). Example: SSO calls
// NewResumeTool(ResumeToolSpec{Name: "pinner_auth_resume", ...}, reg, handles).
func NewResumeTool(spec ResumeToolSpec, reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return ToolDescriptor{
		Name:        spec.Name,
		Title:       "Resume",
		Description: spec.Description,
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[resumeArgs](),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			if reg == nil || handles == nil {
				return NeedsHumanResult(NeedsHuman{
					Reason: ReasonInteractiveOnly,
					Detail: "Resume is not configured for this server.",
				}), nil
			}
			in, err := decodeToolArgs[resumeArgs](req)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}
			if in.Handle == "" {
				return ToolResult{IsError: true, Text: "handle is required"}, nil
			}
			_, data, err := handles.Get(in.Handle)
			if err != nil {
				reg.End(in.Handle)
				detail := spec.UnknownHandleDetail
				if errors.Is(err, ErrHandleExpired) {
					detail = spec.ExpiredHandleDetail
				}
				// A hand-off that can no longer be resumed must not leave the
				// agent retrying a dead handle. Steer it to restart.
				return NeedsHumanResult(NeedsHuman{
					Reason:     spec.deadHandleReason(),
					ResumeTool: spec.RestartTool,
					Detail:     detail,
				}), nil
			}
			cont, ok := reg.Get(in.Handle)
			if !ok {
				// No continuation registered: either never started or already
				// completed. Do not leave the agent polling a futureless handle.
				return NeedsHumanResult(NeedsHuman{
					Reason:     spec.deadHandleReason(),
					ResumeTool: spec.RestartTool,
					Detail:     "This handle has no pending continuation; start a fresh flow.",
				}), nil
			}
			result, err := cont(ctx, in.Handle, data)
			if err != nil {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonCredentialEntry,
					ResumeTool: spec.RestartTool,
					Detail:     "The hand-off failed or expired; start a fresh flow.",
				}), nil
			}
			// A terminal (done) result from the continuation means the hand-off
			// is complete — drop the continuation so it cannot be resumed again.
			if isTerminalResume(result) {
				reg.End(in.Handle)
			}
			return result, nil
		},
	}
}

// isTerminalResume reports whether a resume result is terminal (done) rather
// than a still-pending needs_human. A NeedsHumanResult is never terminal; a
// plain done result is.
func isTerminalResume(result ToolResult) bool {
	if result.StructuredContent == nil {
		return true
	}
	if sc, ok := result.StructuredContent.(map[string]any); ok {
		return sc["status"] != StatusNeedsHuman
	}
	return true
}
