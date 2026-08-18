package model

// This file defines the standard, machine-readable status vocabulary that MCP
// tool results (and the needs_human hand-off) emit in StructuredContent.
// Keeping one vocabulary means agents and other agents can reason about a
// result uniformly: a "status" key always takes one of these values, and an
// "ok"/"pending"/"error" tri-state tells the caller how to proceed.

// Status codes emitted in StructuredContent["status"].
const (
	// StatusOk: the operation completed successfully (terminal).
	StatusOk = "ok"
	// StatusPending: work is in progress; poll a matching *_status tool with
	// the returned handle.
	StatusPending = "pending"
	// StatusRunning: a long-running operation was started and continues
	// asynchronously; a handle is returned for polling.
	StatusRunning = "running"
	// StatusDone: a prior pending/running operation reached its terminal state.
	StatusDone = "done"
	// StatusNeedsHuman: the operation requires human action before it can
	// continue. See the reason/action_url/handle/resume_tool fields.
	StatusNeedsHuman = "needs_human"
	// StatusRequiresAuth: the operation needs authentication the server does
	// not have; the agent should run the OOB sign-in flow first.
	StatusRequiresAuth = "requires_auth"
	// StatusError: the operation failed. See "error" for the machine code.
	StatusError = "error"
)

// Error codes emitted in StructuredContent["error"] when status is StatusError.
const (
	// ErrCodeAuth: authentication failed or is missing.
	ErrCodeAuth = "auth_error"
	// ErrCodeNotFound: a referenced resource/handle/session does not exist.
	ErrCodeNotFound = "not_found"
	// ErrCodeExpired: a handle/session/token has expired.
	ErrCodeExpired = "expired"
	// ErrCodeTimeout: the operation timed out.
	ErrCodeTimeout = "timeout"
	// ErrCodeInvalidArgs: the caller supplied invalid or incomplete arguments.
	ErrCodeInvalidArgs = "invalid_args"
	// ErrCodeUnknown: an otherwise unspecified failure.
	ErrCodeUnknown = "unknown"
)

// StatusResult builds a ToolResult with a machine status. When ok is true the
// status is StatusOk; otherwise StatusError with the given error code. Extra
// key/value pairs are merged into StructuredContent, so callers can add
// domain data (handles, cids, etc.) on top of the standard vocabulary.
func StatusResult(status string, text string, extra map[string]any) ToolResult {
	sc := map[string]any{"status": status}
	for k, v := range extra {
		sc[k] = v
	}
	return ToolResult{Text: text, StructuredContent: sc}
}

// ErrorResult builds a ToolResult flagged IsError=true with a machine error
// code, plus a legible text line.
func ErrorResult(code, text string, extra map[string]any) ToolResult {
	sc := map[string]any{"status": StatusError, "error": code}
	for k, v := range extra {
		sc[k] = v
	}
	return ToolResult{IsError: true, Text: text, StructuredContent: sc}
}

// RequiresAuthResult is the standard response when a tool needs credentials
// the server does not have. It steers the agent to the OOB sign-in flow.
func RequiresAuthResult(detail string) ToolResult {
	return StatusResult(StatusRequiresAuth,
		"Authentication required. Start a sign-in with auth_sso, have the user approve, then retry.",
		map[string]any{"resume_tool": "auth_sso", "detail": detail})
}
