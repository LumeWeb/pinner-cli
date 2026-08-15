package mcp

import (
	"bytes"
	"text/template"
)

// This file provides the standard H2A/A2A hand-off shape: a ToolResult that
// tells the agent a human action (approval, credential entry, confirmation)
// is required before work can continue, and where/how to resume. It is
// SDK-neutral; it builds a plain ToolResult, so the official SDK seam and any
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

// needsHumanTextTemplate renders the plain-text form of a needs_human hand-off.
// It is a text/template so the human-facing copy stays readable and localized
// in one place, rather than being assembled by string concatenation. Each
// segment (URL, resume-with-handle, detail) is optional and omitted when empty.
var needsHumanTextTemplate = template.Must(template.New("needs_human_text").Parse(
	`needs_human: {{.Reason}}{{if .URL}} - open {{.URL}}{{end}}{{if .ResumeTool}} (resume with {{.ResumeTool}}{{if .Handle}}; handle {{.Handle}}{{end}}){{end}}{{if .Detail}} - {{.Detail}}{{end}}`))

// needsHumanTextData are the fields fed to needsHumanTextTemplate.
type needsHumanTextData struct {
	Reason     string
	URL        string
	Handle     string
	ResumeTool string
	Detail     string
}

// needsHumanText builds the plain-text rendering of a needs_human hand-off so a
// text-only tool-calling agent can act on it without parsing StructuredContent.
// It is the single place the human-facing text is assembled; every needs_human
// builder (NeedsHumanResult, vaultHandoffResult, resume continuations) routes
// through it so the text always carries the URL, handle and resume tool the
// agent needs. The handle is always surfaced when present: a caller that only
// sees text must still be able to poll the matching resume tool.
func needsHumanText(reason HandoffReason, url, handle, resumeTool, detail string) string {
	var buf bytes.Buffer
	if err := needsHumanTextTemplate.Execute(&buf, needsHumanTextData{
		Reason:     string(reason),
		URL:        url,
		Handle:     handle,
		ResumeTool: resumeTool,
		Detail:     detail,
	}); err != nil {
		// The template has no user-controlled directives and cannot fail at
		// execution; fall back to a still-actionable bare hand-off rather than
		// propagating a render error into the result.
		return "needs_human: " + string(reason)
	}
	return buf.String()
}

// NeedsHumanResult returns a ToolResult whose structured content carries the
// standard {status:"needs_human", reason, action_url, handle, resume_tool}
// shape, plus a one-line Text for human formatters. It is not an error; a
// needs_human hand-off is a valid, resumable state.
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
	return ToolResult{
		Text:              needsHumanText(n.Reason, n.ActionURL, n.Handle, n.ResumeTool, n.Detail),
		StructuredContent: sc,
	}
}

// Stdin-reading is a CLI-side concern only. The MCP invoke gate does not reason
// about os.Stdin; the agent-facing vault tools are agent-safe OOB hand-offs
// that never touch stdin.
