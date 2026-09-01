package handoff

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// This file provides the generic, internally-shared machinery behind the
// out-of-band hand-off (start -> needs_human -> resume -> done) protocol.
//
// The mechanics are implemented here once and reused by every hand-off flow
// (SSO login, vault seed create, vault seed restore, future OTP/device
// hand-offs):
//   - a start-point op mints a handle and registers a per-handle continuation,
//   - the matching named *_resume tool dispatches on that handle.
//
// Each flow exposes its own named *_resume tool (e.g. auth_resume,
// pinner_vault_resume) so an LLM can recognize the flow from the tool name.
// Only the internals are shared via NewResumeTool.

// ResumeContinuation completes or polls a pending hand-off for a handle. It is
// domain-specific (SSO polls its OOB login; vault polls its seed/restore
// coordinator), registered against the handle when the flow starts. It returns
// either a NeedsHumanResult (hand-off still pending, so the caller should keep
// polling) or a terminal done result (completed).
type ResumeContinuation func(ctx context.Context, handle string, data map[string]any) (model.ToolResult, error)

// HandoffRegistry is the shared internal registry of per-handle resume
// continuations. A start-point tool calls Begin(handle, cont) before returning
// its needs_human hand-off; the template resume tool looks the handle up to
// dispatch. One registry instance is shared by every hand-off flow on the
// server.
//
// The registry has the same bounded, TTL-limited lifetime as AsyncHandleStore:
// a continuation is only valid as long as its backing handle, so entries carry
// a createdAt timestamp and are evicted once past the TTL (lazily on access and
// on each Begin/Prune) and when the map exceeds MaxEntries. An abandoned
// hand-off never leaks a continuation forever.
type HandoffRegistry struct {
	mu         sync.RWMutex
	cont       map[string]registryEntry
	ttp        time.Duration // time-to-live for a registered continuation
	maxEntries int
	now        func() time.Time
	// cleanup, when set, retires the backing async handle when the registry
	// evicts a still-live continuation (capacity eviction). The server injects
	// store.Delete so an evicted flow cannot leave a live-but-unresumable
	// handle that would mislead a resumer.
	cleanup func(handle string)
}

// registryEntry pairs a resume continuation with the instant it was registered
// so expired entries can be evicted under lock.
type registryEntry struct {
	createdAt time.Time
	cont      ResumeContinuation
}

// NewHandoffRegistry returns an empty hand-off continuation registry. Entries
// live for ttl and the registry is capped at maxEntries. Pass DefaultSessionTTL
// and DefaultMaxSessions to match the AsyncHandleStore backing the handles.
func NewHandoffRegistry() *HandoffRegistry {
	return &HandoffRegistry{
		cont:       make(map[string]registryEntry),
		ttp:        session.DefaultSessionTTL,
		maxEntries: session.DefaultMaxSessions,
		now:        time.Now,
	}
}

// SetCleanup wires a callback invoked when the registry evicts a still-live
// continuation to free its backing handle. Concretely the server sets it to the
// AsyncHandleStore.Delete so an evicted flow cannot be resumed into a
// misleading "no pending continuation" steer.
func (r *HandoffRegistry) SetCleanup(fn func(handle string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanup = fn
}

// Begin registers the continuation for a handle. It overwrites any prior
// continuation for the same handle. Call this when a hand-off flow starts,
// BEFORE returning the needs_human hand-off with that handle. Stale entries are
// pruned under the same lock so the registry cannot grow past maxEntries.
func (r *HandoffRegistry) Begin(handle string, c ResumeContinuation) {
	r.mu.Lock()
	// Collect both TTL-expired and capacity-evicted handles, then retire their
	// backing store handles below, OUTSIDE the registry lock (cleanup never
	// touches r.cont and acquires its own store lock).
	pruned := r.pruneLocked()
	evicted := make([]string, 0, 1)
	if len(r.cont) >= r.maxEntries {
		// Bounded like AsyncHandleStore: evict the oldest entry so a flood of
		// abandoned hand-offs cannot exhaust memory. Newer flows win.
		var oldest string
		var oldestAt time.Time
		for h, e := range r.cont {
			if oldest == "" || e.createdAt.Before(oldestAt) {
				oldest, oldestAt = h, e.createdAt
			}
		}
		if oldest != "" {
			delete(r.cont, oldest)
			evicted = append(evicted, oldest)
		}
	}
	r.cont[handle] = registryEntry{createdAt: r.now(), cont: c}
	// Retire the backing handles for the swept flows so none of them can be
	// resumed into a misleading dead-handle steer once their flow is gone. The
	// cleanup callback (AsyncHandleStore.Delete) acquires its OWN store lock,
	// so it must run outside the registry lock to avoid holding every handoff
	// operation hostage to the store and to avoid lock-ordering hazards.
	cleanup := r.cleanup
	r.mu.Unlock()
	if cleanup != nil {
		for _, h := range append(evicted, pruned...) {
			if h != "" {
				cleanup(h)
			}
		}
	}
}

// Get returns the continuation registered for a handle, if any. An entry past
// its TTL is treated as absent, so a dead handle can never be resumed against
// a leaked continuation; expired entries are swept by Begin/Prune.
func (r *HandoffRegistry) Get(handle string) (ResumeContinuation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.cont[handle]
	if !ok {
		return nil, false
	}
	if r.now().Sub(e.createdAt) > r.ttp {
		return nil, false
	}
	return e.cont, true
}

// End removes the continuation for a handle. Call it once the hand-off reaches
// a terminal state so the entry cannot be resumed again.
func (r *HandoffRegistry) End(handle string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cont, handle)
}

// Prune removes every continuation whose backing handle has expired. It is safe
// to call periodically or opportunistically; Get and Begin also self-prune.
func (r *HandoffRegistry) Prune() {
	r.mu.Lock()
	pruned := r.pruneLocked()
	cleanup := r.cleanup
	r.mu.Unlock()
	// Retire backing handles outside the registry lock (cleanup acquires its
	// own store lock and must not run under r.mu).
	if cleanup != nil {
		for _, h := range pruned {
			if h != "" {
				cleanup(h)
			}
		}
	}
}

// pruneLocked removes expired entries, assuming the write lock is held. It
// returns the handles whose entries were removed so the caller can retire their
// backing store handles outside the lock; it does not run cleanup itself.
func (r *HandoffRegistry) pruneLocked() []string {
	now := r.now()
	var evicted []string
	for h, e := range r.cont {
		if now.Sub(e.createdAt) > r.ttp {
			delete(r.cont, h)
			evicted = append(evicted, h)
		}
	}
	return evicted
}

// resumeArgs is the input of any *_resume tool.
type resumeArgs struct {
	// Handle is the handle returned by the matching start tool.
	Handle string `json:"handle" jsonschema:"required,description=The handle returned by the matching start tool, used to poll the pending hand-off."`
}

// ResumeToolSpec carries the per-domain copy + restart steering for a *_resume
// tool built from the shared template. The dispatch LOGIC is shared (see
// NewResumeTool); the description and the dead-handle guidance text are
// domain-flavored so an agent sees the right restart instruction for the flow
// it is in (SSO says "start a fresh login", vault says "start a fresh setup").
type ResumeToolSpec struct {
	// Name is the tool name (e.g. "auth_resume").
	Name string
	// Title is the short human/agent-facing title shown in tools/list. It must
	// be flow-specific (e.g. "Auth Sign-In Resume", "Vault Create Resume") so
	// the SSO, vault-create and vault-restore resume tools are distinguishable
	// in a host UI rather than all rendering as "Resume".
	Title string
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
	DeadHandleReason model.HandoffReason
	// Category is the tool category for search/filter. Defaults to CategoryCore
	// when zero. Auth resumes use CategoryAccount; vault resumes use
	// CategoryVault so filtering by category surfaces the OOB flow correctly.
	Category model.ToolCategory
}

// CategoryOrDefault returns the resume tool's category, defaulting to
// CategoryCore when the spec left it unset.
func (s ResumeToolSpec) CategoryOrDefault() model.ToolCategory {
	if s.Category == "" {
		return model.CategoryCore
	}
	return s.Category
}

// title returns the flow-specific title, defaulting to a plain "Resume" only if
// a domain did not supply one (all current callers do).
func (s ResumeToolSpec) title() string {
	if s.Title != "" {
		return s.Title
	}
	return "Resume"
}

// deadHandleReason returns the domain's HandoffReason for steering an agent
// away from a dead/unresumable handle, defaulting to ReasonCredentialEntry.
func (s ResumeToolSpec) deadHandleReason() model.HandoffReason {
	if s.DeadHandleReason != "" {
		return s.DeadHandleReason
	}
	return model.ReasonCredentialEntry
}

// NewResumeTool builds a per-domain, named *_resume tool from the shared
// template. The resume dispatch logic is implemented here once:
//
//   - decode + require the handle,
//   - AsyncHandleStore Get for TTL/expiry (a missing/expired handle steers the
//     agent to restart the flow instead of retrying a dead handle),
//   - HandoffRegistry lookup to dispatch to the domain's ResumeContinuation,
//   - a continuation returning pending (needs_human) vs terminal (done).
//
// A domain supplies its tool spec (name, description, restart steering, and
// dead-handle guidance text). Example: SSO calls
// NewResumeTool(ResumeToolSpec{Name: "auth_resume", ...}, reg, handles).
func NewResumeTool(spec ResumeToolSpec, reg *HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        spec.Name,
		Title:       spec.title(),
		Description: spec.Description,
		Category:    spec.CategoryOrDefault(),
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(spec.Description)),
		InputSchema: toolargs.ToolSchemaFor[resumeArgs](),
		// OpenAI tool invocation labels shown by UI-capable hosts while the
		// tool runs and after it finishes. Required alongside the openai
		// outputTemplate the app-helper status variants carry.
		Meta: map[string]any{
			"openai/toolInvocation": map[string]any{
				"invoking": "Checking hand-off status…",
				"invoked":  "Hand-off status checked",
			},
		},
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			if reg == nil || handles == nil {
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason: model.ReasonInteractiveOnly,
					Detail: "Resume is not configured for this server.",
				}), nil
			}
			in, err := toolargs.DecodeToolArgs[resumeArgs](req)
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}
			if in.Handle == "" {
				return model.ToolResult{IsError: true, Text: "handle is required"}, nil
			}
			_, data, err := handles.Get(in.Handle)
			if err != nil {
				reg.End(in.Handle)
				detail := spec.UnknownHandleDetail
				if errors.Is(err, session.ErrHandleExpired) {
					detail = spec.ExpiredHandleDetail
				}
				// A hand-off that can no longer be resumed must not leave the
				// agent retrying a dead handle. Steer it to restart.
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason:     spec.deadHandleReason(),
					ResumeTool: spec.RestartTool,
					Detail:     detail,
				}), nil
			}
			cont, ok := reg.Get(in.Handle)
			if !ok {
				// No continuation registered: either never started or already
				// completed. Do not leave the agent polling a futureless handle.
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason:     spec.deadHandleReason(),
					ResumeTool: spec.RestartTool,
					Detail:     "This handle has no pending continuation; start a fresh flow.",
				}), nil
			}
			result, err := cont(ctx, in.Handle, data)
			if err != nil {
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason:     model.ReasonCredentialEntry,
					ResumeTool: spec.RestartTool,
					Detail:     "The hand-off failed or expired; start a fresh flow.",
				}), nil
			}
			// A terminal (done) result from the continuation means the hand-off
			// is complete. Drop the continuation so it cannot be resumed again.
			if isTerminalResume(result) {
				reg.End(in.Handle)
			}
			return result, nil
		},
	}
}

// isTerminalResume reports whether a resume result is terminal (done) rather
// than a still-pending needs_human. Only an explicit status that is not
// needs_human is terminal. A result with nil or non-map structured content is
// treated as UNKNOWN (not terminal): keeping the continuation makes the agent
// poll once more, which is harmless, whereas dropping it mid-flow would report
// "done" for a flow that may still be pending. SSO returns map content for both
// pending and done, so this is only a safety net for future generic flows
// (vault seed, OTP).
func isTerminalResume(result model.ToolResult) bool {
	sc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return false
	}
	status, _ := sc["status"].(string)
	return status != "" && status != model.StatusNeedsHuman
}
