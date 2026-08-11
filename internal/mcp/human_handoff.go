package mcp

import "os"

// This file provides the standard H2A/A2A hand-off shape: a ToolResult that
// tells the agent a human action (approval, credential entry, confirmation)
// is required before work can continue, and where/how to resume. It is
// SDK-neutral — it builds a plain ToolResult, so the official SDK seam and any
// other transport encode it consistently.

// HandoffReason enumerates why a tool is asking for human intervention.
type HandoffReason string

const (
	// ReasonSSOApproval: a browser-based out-of-band login is pending; the
	// human must open ActionURL and approve.
	ReasonSSOApproval HandoffReason = "sso_approval"
	// ReasonInteractiveOnly: the underlying command is human-only (interactive
	// prompt) with no agent-safe form; the human must run it via the CLI.
	ReasonInteractiveOnly HandoffReason = "interactive_only"
	// ReasonStdinRequired: the command needs piped stdin (e.g. a seed or file
	// content) that the MCP channel cannot provide; the human or a host
	// mechanism must supply it.
	ReasonStdinRequired HandoffReason = "stdin_required"
	// ReasonCredentialEntry: a credential/token must be supplied.
	ReasonCredentialEntry HandoffReason = "credential_entry"
	// ReasonConfirmation: a destructive or consequential action needs
	// confirmation.
	ReasonConfirmation HandoffReason = "confirmation"
)

// NeedsHuman describes a tool result that requires human action before work
// can continue. It is the standard agent-facing hand-off so agents (and other
// agents) know exactly what to do next.
type NeedsHuman struct {
	// Status is always "needs_human" when serialized.
	Reason     HandoffReason `json:"reason"`
	ActionURL  string        `json:"action_url,omitempty"`  // short-lived URL the human opens (omit for non-URL reasons)
	Handle     string        `json:"handle,omitempty"`      // async handle used by a matching resume/status tool
	ResumeTool string        `json:"resume_tool,omitempty"` // tool name to poll/resume with
	Detail     string        `json:"detail,omitempty"`      // optional human-readable context
}

// NeedsHumanResult returns a ToolResult whose structured content carries the
// standard {status:"needs_human", reason, action_url, handle, resume_tool}
// shape, plus a legible one-line Text for human formatters. It is not an
// error — a needs_human hand-off is a valid, resumable state.
func NeedsHumanResult(n NeedsHuman) ToolResult {
	sc := map[string]any{
		"status": StatusNeedsHuman,
		"reason": n.Reason,
	}
	if n.ActionURL != "" {
		sc["action_url"] = n.ActionURL
	}
	if n.Handle != "" {
		sc["handle"] = n.Handle
	}
	if n.ResumeTool != "" {
		sc["resume_tool"] = n.ResumeTool
	}
	if n.Detail != "" {
		sc["detail"] = n.Detail
	}
	text := "needs_human: " + string(n.Reason)
	if n.ActionURL != "" {
		text += " - open " + n.ActionURL
	}
	if n.ResumeTool != "" {
		text += " (resume with " + n.ResumeTool + ")"
	}
	if n.Detail != "" {
		text += " - " + n.Detail
	}
	return ToolResult{Text: text, StructuredContent: sc}
}

// stdinHasData reports whether stdin has piped data available WITHOUT blocking.
// Over the MCP channel stdin is drained or absent for agent invocations, so a
// nonzero-char-device check tells invoke_tool whether a stdin-input command can
// proceed or must be redirected. It mirrors pkg/cli.isStdinPipe semantics.
func stdinHasData() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// A pipe/socket/file (not a character device/terminal) counts as having
	// stdin available.
	return (fi.Mode() & os.ModeCharDevice) == 0
}
