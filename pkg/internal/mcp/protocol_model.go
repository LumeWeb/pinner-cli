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
	Handler     PinnerToolHandler
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
