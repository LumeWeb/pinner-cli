package mcp

import (
	"context"
	"encoding/json"
)

// ToolRequest is the SDK-neutral input to a catalog tool.
type ToolRequest struct {
	Name      string
	Arguments map[string]any
	// InputResponses reports whether this invocation is a retry that carried
	// form-elicitation input (InputResponses on the wire) from the client, as
	// opposed to a fresh argument-only call. Handlers use it to tell a failed
	// form submission apart from a first-time call so they can re-present the
	// native form instead of falling back to plain text.
	InputResponses bool
}

// ToolResult is the SDK-neutral result of a catalog tool.
type ToolResult struct {
	IsError           bool
	Text              string
	StructuredContent any
	// Elicitation, when set, asks the connected client for interactive input
	// (a form or URL) instead of returning a terminal result. The SDK seam
	// converts it to an input_required response.
	Elicitation *ElicitationSpec
}

// PinnerToolHandler executes a catalog tool without depending on an MCP SDK.
type PinnerToolHandler func(context.Context, ToolRequest) (ToolResult, error)

// ToolBehavior captures the agent-facing execution behavior of a tool. The
// invoke gate and post-processing layers used to encode these as hardcoded
// tool-name / argument-string checks (e.g. "pinner_vault_restore" +
// "seed-stdin"); carrying them declaratively on the entry turns those into
// pure data-driven switches.
type ToolBehavior struct {
	// StdinGate, when non-nil, names the argument that switches this tool
	// from its agent-safe hand-off into a raw os.Stdin read. An invocation
	// with that argument truthy must be gated (redirected rather than run,
	// which would consume the MCP transport pipe); an invocation without it
	// is agent-safe. This replaces the hardcoded pinner_vault_restore /
	// seed-stdin bypass in invoke_tool.
	StdinGate *StdinGateSpec

	// RestoreURL, when non-nil, marks this tool as an out-of-band restore
	// that mints a one-time /restore/<token> browser URL for the human to
	// supply the seed. Its presence is what exempts a non-stdin invocation
	// from its own StdinGate (the OOB path is reachable and must not be
	// wrongly redirected when stdin is /dev/null).
	RestoreURL *RestoreURLSpec

	// SeedDrop, when non-nil, marks this tool as one whose stdout carries a
	// vault-recovery-seed path that the MCP layer should turn into a one-time
	// browser hand-off instead of returning the mnemonic on the channel.
	SeedDrop *SeedDropSpec
}

// StdinGateSpec names the single boolean argument that flips a tool into a
// raw os.Stdin read (e.g. seed-stdin on vault restore).
type StdinGateSpec struct {
	// ArgName is the flag key (as it appears in the JSON arguments map) that
	// triggers the stdin path.
	ArgName string
}

// SeedDropSpec describes how to build a one-time seed-drop URL from a tool's
// agent-mode JSON output.
type SeedDropSpec struct {
	// ProfileField is the JSON field carrying the profile name.
	ProfileField string
	// SeedPathField is the JSON field carrying the host seed-file path.
	SeedPathField string
}

// RestoreURLSpec describes how to build a one-time restore URL from a tool's
// agent-mode JSON output.
type RestoreURLSpec struct {
	// ProfileField is the JSON field carrying the profile name.
	ProfileField string
}

// ToolDescriptor describes a Pinner-owned tool.
type ToolDescriptor struct {
	Name        string
	Title       string
	Description string
	Category    ToolCategory
	ReadOnly    bool
	Destructive bool
	InputSchema json.RawMessage
	Meta        map[string]any
	// Behavior captures agent-facing execution behavior (stdin gating, OOB
	// hand-offs) that used to be encoded as hardcoded tool-name checks in the
	// invoke gate and post-processing layers. Carrying it on the descriptor
	// keeps the two discovery surfaces (direct tools/list and the internal
	// catalog) in sync through the shared converters.
	Behavior ToolBehavior
	Handler  PinnerToolHandler
}

func descriptorFromTool(entry *ToolEntry) ToolDescriptor {
	return ToolDescriptor{
		Name:        entry.Name,
		Title:       entry.Title,
		Description: entry.Description,
		Category:    entry.Category,
		ReadOnly:    entry.ReadOnly,
		Destructive: entry.Destructive,
		InputSchema: entry.InputSchema,
		Meta:        entry.Meta,
		Behavior:    entry.Behavior,
	}
}

// toolEntryFromDescriptor mirrors descriptorFromTool in the reverse direction.
// It lets a tool that is registered as a direct (tools/list) descriptor, such
// as the out-of-band sign-in tools, ALSO be surfaced through progressive
// discovery (search_tools/describe_tool) so both discovery surfaces stay in
// sync. The entry keeps its handler so invoke_tool can call it.
func toolEntryFromDescriptor(desc ToolDescriptor) *ToolEntry {
	return &ToolEntry{
		Name:        desc.Name,
		Title:       desc.Title,
		Description: desc.Description,
		Category:    desc.Category,
		ReadOnly:    desc.ReadOnly,
		Destructive: desc.Destructive,
		InputSchema: desc.InputSchema,
		Meta:        desc.Meta,
		Behavior:    desc.Behavior,
		Handler:     desc.Handler,
		// Direct auth tools are non-blocking and safe for agent invocation:
		// pinner_auth_sso returns a needs_human hand-off, pinner_auth_resume
		// polls. Ensure discovery treats them as callable, not interactive.
		Interaction: InteractionAgentSafe,
	}
}

// ResourceResult is the SDK-neutral resource response.
type ResourceResult struct {
	URI      string
	MIMEType string
	Text     string
}

// PromptMessage is the SDK-neutral prompt message.
type PromptMessage struct {
	Role             string
	Text             string
	EmbeddedResource *ResourceResult
}

// PromptResult is the SDK-neutral prompt response.
type PromptResult struct {
	// Description is an optional description returned in the prompt result.
	Description string
	Messages    []PromptMessage
}

// ResourceRequest is the SDK-neutral input to a resource or resource-template
// handler. Arguments carries the URI-template substitution values.
type ResourceRequest struct {
	URI       string
	Arguments map[string]string
}

// ResourceHandler executes a resource or resource-template read without
// depending on an MCP SDK.
type ResourceHandler func(context.Context, ResourceRequest) (ResourceResult, error)

// ResourceDescriptor describes a known pinner:// resource.
type ResourceDescriptor struct {
	URI         string
	Name        string
	Title       string
	Description string
	MIMEType    string
	Handler     ResourceHandler
}

// ResourceTemplateDescriptor describes a pinner:// URI template.
type ResourceTemplateDescriptor struct {
	URITemplate string
	Name        string
	Title       string
	Description string
	MIMEType    string
	Handler     ResourceHandler
}

// PromptRequest is the SDK-neutral input to a prompt handler.
type PromptRequest struct {
	Arguments map[string]string
}

// PromptArgumentDescriptor describes a single prompt argument.
type PromptArgumentDescriptor struct {
	Name        string
	Title       string
	Description string
	Required    bool
}

// PromptDescriptor describes a prompt template.
type PromptDescriptor struct {
	Name        string
	Title       string
	Description string
	Arguments   []PromptArgumentDescriptor
	Handler     func(context.Context, PromptRequest) (PromptResult, error)
}

// toolDescriptor returns the SDK-neutral view of a catalog entry.
func toolDescriptor(entry *ToolEntry) ToolDescriptor {
	return descriptorFromTool(entry)
}

// toolDescriptorForHandler attaches a Pinner-owned handler to a descriptor.
func toolDescriptorForHandler(entry *ToolEntry, handler PinnerToolHandler) ToolDescriptor {
	descriptor := descriptorFromTool(entry)
	descriptor.Handler = handler
	return descriptor
}
