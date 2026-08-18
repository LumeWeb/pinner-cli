package model

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
	// Caps is the per-request capability view of the calling client, derived
	// from the request _meta (stateless MCP) or the legacy initialize
	// handshake. It lets a single server adapt behavior per request (e.g. a
	// tool handler returning a real result vs a UI placeholder) without
	// reintroducing session state. Nil on code paths that do not carry a
	// client (e.g. tests invoking handlers directly).
	Caps *RequestCaps
}

// RequestCaps is the SDK-neutral per-request capability view of the calling
// client. MCP is stateless: capabilities travel with each request rather than
// being bound to a connection, so this is re-derived for every invocation.
type RequestCaps struct {
	// ProtocolVersion is the MCP protocol version the client sent on this
	// request. Empty when the client did not advertise one.
	ProtocolVersion string
	// ClientName and ClientVersion identify the calling client (clientInfo).
	ClientName    string
	ClientVersion string
	// UI is the typed MCP Apps capability when the client advertises
	// io.modelcontextprotocol/ui, else nil.
	UI *ClientUICapabilities
}

// SupportsApps reports whether the calling client can render MCP Apps
// (RESOURCE_MIME_TYPE ui:// resources). It is the per-request counterpart to
// ClientUICapabilities.SupportsApps.
func (c *RequestCaps) SupportsApps() bool {
	return c != nil && c.UI != nil && c.UI.SupportsApps()
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

// SecurityScheme describes a tool's authentication policy to the host. It is
// the SDK-neutral form of the OpenAI per-tool `securitySchemes` declaration:
// whether a tool may run anonymously (`noauth`) or requires an OAuth 2.0 access
// token (`oauth2`), and which scopes the token must carry. Pinner runs its whole
// MCP server behind a protected resource, so tools default to oauth2 with no
// application scopes; individual tools that may run anonymously declare noauth.
type SecurityScheme struct {
	// Type is "noauth" or "oauth2".
	Type string `json:"type"`
	// Scopes enumerates the OAuth scopes the tool requires. Empty for a
	// server where the auth server issues no distinct application scopes.
	// Always serialized (even empty) so the oauth2 declaration carries an
	// explicit `scopes` array per the OpenAI tool-auth contract.
	Scopes []string `json:"scopes"`
}

// ToolDescriptor describes a Pinner-owned tool.
type ToolDescriptor struct {
	Name        string
	Title       string
	Description string
	Category    ToolCategory
	ReadOnly    bool
	Destructive bool
	// DirectVisible reports whether the tool is part of the directly-exposed
	// surface (tools/list) in addition to progressive discovery. It replaces
	// the adhoc name-allowlist predicate (IsCuratedTool) used to filter the
	// curated registration loop.
	DirectVisible bool
	InputSchema   json.RawMessage
	// OutputSchema is the JSON Schema describing the tool's StructuredContent
	// (the shape of a successful CallToolResult). The wire seam emits it as the
	// official tool's `outputSchema` so the descriptor matches what the handler
	// actually returns. When nil, no outputSchema is declared on the wire.
	OutputSchema json.RawMessage
	Meta         map[string]any
	// SecuritySchemes declares the tool's auth policy. When nil, the wire seam
	// applies the server default (oauth2, no scopes) because Pinner's MCP
	// server is a protected resource.
	SecuritySchemes []SecurityScheme
	// SensitiveFlags mirrors ToolEntry.SensitiveFlags so descriptor-registered
	// tools that carry credential-bearing flags surface them to the redaction
	// path through the shared converters.
	SensitiveFlags []string
	Handler        PinnerToolHandler
}

// DescriptorFromTool builds the SDK-neutral descriptor for a catalog entry.
func DescriptorFromTool(entry *ToolEntry) ToolDescriptor {
	return ToolDescriptor{
		Name:            entry.Name,
		Title:           entry.Title,
		Description:     entry.Description,
		Category:        entry.Category,
		ReadOnly:        entry.ReadOnly,
		Destructive:     entry.Destructive,
		DirectVisible:   entry.DirectVisible,
		InputSchema:     entry.InputSchema,
		OutputSchema:    entry.OutputSchema,
		Meta:            entry.Meta,
		SecuritySchemes: entry.SecuritySchemes,
		SensitiveFlags:  entry.SensitiveFlags,
	}
}

// ToolEntryFromDescriptor mirrors DescriptorFromTool in the reverse direction.
// It lets a tool that is registered as a direct (tools/list) descriptor, such
// as the out-of-band sign-in tools, ALSO be surfaced through progressive
// discovery (search_tools/describe_tool) so both discovery surfaces stay in
// sync. The entry keeps its handler so invoke_tool can call it.
func ToolEntryFromDescriptor(desc ToolDescriptor) *ToolEntry {
	return &ToolEntry{
		Name:            desc.Name,
		Title:           desc.Title,
		Description:     desc.Description,
		Category:        desc.Category,
		ReadOnly:        desc.ReadOnly,
		Destructive:     desc.Destructive,
		DirectVisible:   desc.DirectVisible,
		InputSchema:     desc.InputSchema,
		OutputSchema:    desc.OutputSchema,
		Meta:            desc.Meta,
		SecuritySchemes: desc.SecuritySchemes,
		SensitiveFlags:  desc.SensitiveFlags,
		Handler:         desc.Handler,
		// Direct auth tools are non-blocking and safe for agent invocation:
		// auth_sso returns a needs_human hand-off, auth_resume
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

// ToolDescriptorFromEntry returns the SDK-neutral view of a catalog entry.
func ToolDescriptorFromEntry(entry *ToolEntry) ToolDescriptor {
	return DescriptorFromTool(entry)
}

// ToolDescriptorForHandler attaches a Pinner-owned handler to a descriptor.
func ToolDescriptorForHandler(entry *ToolEntry, handler PinnerToolHandler) ToolDescriptor {
	descriptor := DescriptorFromTool(entry)
	descriptor.Handler = handler
	return descriptor
}
