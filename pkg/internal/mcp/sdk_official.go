// Official MCP SDK adapter.
//
// This file is the only file in the package that imports
// github.com/modelcontextprotocol/go-sdk. It converts Pinner-owned
// descriptors (defined in protocol_model.go) into registrations on the
// official MCP server, preserving Pinner's wire JSON contract exactly:
//
//   - the three visible meta-tools (search_tools, describe_tool,
//     invoke_tool) and their serialized schemas;
//   - the progressive-disclosure catalog invocation behavior;
//   - pinner:// resource and resource-template URIs, MIME types and payloads;
//   - prompt names, arguments, roles, text and embedded resources.
//
// The catalog, wizard, resource-provider, prompt and OAuth domain logic must
// NOT import either MCP SDK. They speak Pinner-owned descriptors and handlers;
// this adapter is the only bridge to the protocol implementation.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"github.com/yosida95/uritemplate/v3"
)

// build.Version is stamped by ldflags. Fall back to a dev constant when it is
// empty (e.g. during `go test`).
const officialSDKVersion = "v1.4.1"

// OfficialServer is the SDK-neutral alias for the official MCP server type. It
// lets the rest of the package (which must not import the official SDK) carry
// an official server through the public serving path without a name collision.
type OfficialServer = mcp.Server

// OfficialImplementation returns the Pinner server implementation descriptor
// used to initialize an official-SDK server.
func OfficialImplementation() *mcp.Implementation {
	return &mcp.Implementation{
		Name:    "pinner",
		Version: "0.0.0-dev",
	}
}

// OfficialServerOptions is the SDK-neutral surface for official server
// options. Zero fields map to official SDK defaults.
type OfficialServerOptions struct {
	Instructions string
}

func officialServerOptions(opts *OfficialServerOptions) *mcp.ServerOptions {
	if opts == nil {
		return nil
	}
	return &mcp.ServerOptions{Instructions: opts.Instructions}
}

// NewOfficialServer builds an official-SDK MCP server pre-configured with
// Pinner's identity. Feature registration is performed separately with
// RegisterOfficialMetaTools, RegisterOfficialResources and RegisterOfficialPrompts.
func NewOfficialServer(opts *OfficialServerOptions) *mcp.Server {
	return mcp.NewServer(OfficialImplementation(), officialServerOptions(opts))
}

// OfficialServerFromCatalog builds the official server with Pinner's
// progressive-disclosure meta-tools. The catalog remains internal.
func OfficialServerFromCatalog(catalog *ToolCatalog, instructions string) (*mcp.Server, error) {
	if catalog == nil {
		return nil, fmt.Errorf("nil tool catalog")
	}
	srv := NewOfficialServer(&OfficialServerOptions{Instructions: instructions})
	if err := RegisterOfficialMetaTools(srv, catalog); err != nil {
		return nil, err
	}
	return srv, nil
}

// OfficialMCPServer builds an MCP server from a urfave/cli/v3 command tree.
// It populates a ToolCatalog and exposes it through the three
// progressive-disclosure meta-tools. This is the server the public MCPCommand
// serves over stdio / streamable-HTTP.
//
// Resources and prompts are registered by the command action after runtime
// providers and options are resolved. The descriptor adapters below preserve
// their wire contracts on the official server.
func OfficialMCPServer(root *cli.Command, hasRootAction bool, prefix []string, seedDrop *SeedDrop, oobRestore *OOBRestore) (*mcp.Server, *ToolCatalog, error) {
	catalog, err := buildCatalog(root, hasRootAction, prefix, seedDrop, oobRestore)
	if err != nil {
		return nil, nil, err
	}
	srv, err := OfficialServerFromCatalog(catalog, buildInstructions(catalog.Len()))
	if err != nil {
		return nil, nil, err
	}
	return srv, catalog, nil
}

// officialStdioTransport builds a transport bound to the given stdin/stdout
// readers/writers. os.Stdin/os.Stdout satisfy io.ReadCloser / io.WriteCloser
// respectively, so callers typically pass them directly.
func officialStdioTransport(r io.ReadCloser, w io.WriteCloser) *mcp.IOTransport {
	return &mcp.IOTransport{Reader: r, Writer: w}
}

// RunOfficialStdio serves an official-SDK MCP server over the stdio transport
// bound to the given stdin/stdout, blocking until ctx is cancelled or the
// client closes the stream. This keeps the official SDK types out of
// adapter.go's public Serving path.
func RunOfficialStdio(ctx context.Context, srv *mcp.Server, r io.ReadCloser, w io.WriteCloser) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	return srv.Run(ctx, officialStdioTransport(r, w))
}

// ---------------------------------------------------------------------------
// Tool conversion
// ---------------------------------------------------------------------------

// officialTool converts a Pinner-owned tool descriptor into an official SDK
// tool. The raw CLI-generated input schema is preserved verbatim; annotations
// annotations (readOnlyHint/destructiveHint/title) are carried in ToolAnnotations.
func officialTool(desc ToolDescriptor) *mcp.Tool {
	tool := &mcp.Tool{
		Name:        desc.Name,
		Description: desc.Description,
		Title:       desc.Title,
		InputSchema: json.RawMessage(desc.InputSchema),
		Meta:        desc.Meta,
	}
	if desc.ReadOnly || desc.Destructive || desc.Title != "" {
		tool.Annotations = &mcp.ToolAnnotations{
			Title:           desc.Title,
			ReadOnlyHint:    desc.ReadOnly,
			DestructiveHint: &desc.Destructive,
		}
	}
	return tool
}

// PinnerToolHandler → mcp.ToolHandler. The arguments arrive on the wire as
// raw JSON; unmarshal them into a plain map for the Pinner-owned handler. When
// the call is a retry after an input_required elicitation, the accepted form
// content is merged into the arguments under their elicitation id so handlers
// read form submissions like any other argument.
func officialToolHandler(handler PinnerToolHandler) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if req.Params.Arguments != nil {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid arguments: %v", err)}},
				}, nil
			}
		}
		for id, content := range acceptedElicitations(req) {
			args[id] = content
		}
		// Recover cross-round state the client echoed back on a retry after an
		// input_required result, so handlers can re-establish context (e.g. a
		// session id) even if the client did not echo the original arguments.
		if req.Params.RequestState != "" {
			if _, ok := args["request_state"]; !ok {
				args["request_state"] = req.Params.RequestState
			}
		}
		result, err := handler(ctx, ToolRequest{
			Name:           req.Params.Name,
			Arguments:      args,
			InputResponses: len(req.Params.InputResponses) > 0,
		})
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}
		return officialToolResult(result), nil
	}
}

// officialToolResult converts a Pinner-owned tool result into an official SDK
// result, preserving isError and single text-content semantics. When the result
// carries an Elicitation it is converted into an input_required response.
func officialToolResult(result ToolResult) *mcp.CallToolResult {
	if result.Elicitation != nil {
		return callToolResultFromElicitation(*result.Elicitation)
	}
	return &mcp.CallToolResult{
		IsError:           result.IsError,
		Content:           []mcp.Content{&mcp.TextContent{Text: result.Text}},
		StructuredContent: result.StructuredContent,
	}
}

// registerTool is the single registration seam for Pinner-owned tools. It
// applies the handler adaptation (officialToolHandler) in one place, so
// callers no longer hand-roll srv.AddTool(tool, officialToolHandler(handler)).
func registerTool(srv *mcp.Server, tool *mcp.Tool, handler PinnerToolHandler) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	srv.AddTool(tool, officialToolHandler(handler))
	return nil
}

// RegisterOfficialMetaTools registers the three progressive-disclosure
// meta-tools (search_tools, describe_tool, invoke_tool) on an official-SDK
// server. The catalog itself stays hidden; the only tools visible via
// tools/list are these three, preserving progressive disclosure.
func RegisterOfficialMetaTools(srv *mcp.Server, catalog *ToolCatalog) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}

	if err := registerOfficialSearchTools(srv, catalog); err != nil {
		return err
	}
	if err := registerOfficialDescribeTool(srv, catalog); err != nil {
		return err
	}
	return registerOfficialInvokeTool(srv, catalog)
}

// metaToolSchema is a tiny SDK-neutral input schema builder for the static
// meta-tools.
type metaToolSchema struct {
	props map[string]any
}

func (s *metaToolSchema) property(name string, schema map[string]any) {
	if s.props == nil {
		s.props = make(map[string]any)
	}
	s.props[name] = schema
}

func (s *metaToolSchema) raw() json.RawMessage {
	obj := map[string]any{"type": "object", "properties": s.props}
	if s.props == nil {
		obj["properties"] = map[string]any{}
	}
	out, _ := json.Marshal(obj)
	return out
}

// searchToolsInput is the typed argument shape for search_tools.
type searchToolsInput struct {
	Query    string `json:"query,omitempty" jsonschema:"description=Keywords to search for in tool names and descriptions."`
	Category string `json:"category,omitempty" jsonschema:"description=Filter by category: core, admin, or wizard."`
}

// describeToolInput is the typed argument shape for describe_tool.
type describeToolInput struct {
	Name string `json:"name" jsonschema:"description=Tool name from search_tools result."`
}

// invokeToolInput is the typed argument shape for invoke_tool.
type invokeToolInput struct {
	Name      string         `json:"name" jsonschema:"description=Tool name from search_tools result."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"description=Arguments object matching the tool's inputSchema."`
}

func registerOfficialSearchTools(srv *mcp.Server, catalog *ToolCatalog) error {
	schema := &metaToolSchema{}
	schema.property("query", map[string]any{
		"type":        "string",
		"description": "Keywords to search for in tool names and descriptions. Supports subsequence matching (e.g. 'pload' matches 'pinner_upload'). Leave empty to return all tools.",
	})
	schema.property("category", map[string]any{
		"type":        "string",
		"description": "Filter by category: 'core' (user commands), 'admin' (admin operations), 'wizard' (interactive wizards). Leave empty to search all categories.",
	})

	tool := &mcp.Tool{
		Name:        "search_tools",
		Description: "Search the internal tool catalog by keyword. Returns matching tool names, descriptions, and categories (without input schemas). Use describe_tool to get the full input schema for a specific tool. Leave query empty to list all available tools.",
		InputSchema: schema.raw(),
	}

	handler := PinnerToolHandler(func(_ context.Context, request ToolRequest) (ToolResult, error) {
		in, err := decodeToolArgs[searchToolsInput](request)
		if err != nil {
			return ToolResult{}, err
		}
		summaries := catalog.Search(in.Query, in.Category)
		data, err := json.Marshal(map[string]any{"tools": summaries, "total": len(summaries)})
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Text: string(data)}, nil
	})

	return registerTool(srv, tool, handler)
}

func registerOfficialDescribeTool(srv *mcp.Server, catalog *ToolCatalog) error {
	schema := &metaToolSchema{}
	schema.property("name", map[string]any{
		"type":        "string",
		"description": "Tool name from search_tools result",
	})

	tool := &mcp.Tool{
		Name:        "describe_tool",
		Description: "Get the full input schema for a single tool by name. Use the tool name returned by search_tools. The inputSchema field contains the JSON Schema that the tool's arguments must conform to.",
		InputSchema: schema.raw(),
	}

	handler := PinnerToolHandler(func(_ context.Context, request ToolRequest) (ToolResult, error) {
		in, err := decodeToolArgs[describeToolInput](request)
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		if in.Name == "" {
			return ToolResult{IsError: true, Text: "name is required"}, nil
		}
		detail, err := catalog.Describe(in.Name)
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		data, err := json.Marshal(detail)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Text: string(data)}, nil
	})

	return registerTool(srv, tool, handler)
}

func registerOfficialInvokeTool(srv *mcp.Server, catalog *ToolCatalog) error {
	schema := &metaToolSchema{}
	schema.property("name", map[string]any{
		"type":        "string",
		"description": "Tool name from search_tools result",
	})
	schema.property("arguments", map[string]any{
		"type":        "object",
		"description": "Arguments object matching the tool's inputSchema. Use describe_tool to see the schema.",
	})

	tool := &mcp.Tool{
		Name:        "invoke_tool",
		Description: "Execute a tool by name with the given arguments. Use describe_tool first to discover the required argument schema. The arguments object must match the tool's inputSchema.",
		InputSchema: schema.raw(),
	}

	handler := PinnerToolHandler(func(ctx context.Context, request ToolRequest) (ToolResult, error) {
		in, err := decodeToolArgs[invokeToolInput](request)
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		if in.Name == "" {
			return ToolResult{IsError: true, Text: "name is required"}, nil
		}
		toolArgs := in.Arguments
		if toolArgs == nil {
			toolArgs = map[string]any{}
		}
		entry, ok := catalog.Get(in.Name)
		if !ok {
			return ToolResult{IsError: true, Text: fmt.Sprintf("unknown tool: %s", in.Name)}, nil
		}

		// Steer agents away from commands they cannot run safely over the MCP
		// channel, instead of letting them hang. A human-only (interactive)
		// command always redirects; a stdin-input command redirects unless piped
		// data is actually available. Everything else runs normally.
		switch entry.Interaction {
		case InteractionInteractive:
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonInteractiveOnly,
				ResumeTool: "",
				Detail:     "This command is human-only (it prompts interactively) and has no agent-safe form. Run it via the CLI, or use the curated agent tool for the same workflow.",
			}), nil
		case InteractionStdinInput:
			if !stdinHasData() {
				return NeedsHumanResult(NeedsHuman{
					Reason:     ReasonStdinRequired,
					ResumeTool: "",
					Detail:     "This command reads piped stdin, which the MCP channel cannot supply, and has no agent-safe stdin path. A human or host process must run it on the MCP server host with the required input piped in.",
				}), nil
			}
		}

		result, err := entry.Handler(ctx, ToolRequest{Name: in.Name, Arguments: toolArgs})
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		return result, nil
	})

	return registerTool(srv, tool, handler)
}

// RegisterOfficialToolsFromCatalog registers every catalog entry as an
// official tool with its Pinner-owned handler. Pinner keeps these hidden from
// tools/list by design (progressive disclosure); this exists for callers that
// opt into first-class exposure.
func RegisterOfficialToolsFromCatalog(srv *mcp.Server, catalog *ToolCatalog) error {
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	return RegisterOfficialMetaTools(srv, catalog)
}

// RegisterOfficialDescriptor adds one Pinner-owned tool directly to tools/list.
func RegisterOfficialDescriptor(srv *mcp.Server, desc ToolDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if desc.Name == "" || desc.Handler == nil {
		return fmt.Errorf("direct tool requires name and handler")
	}
	return registerTool(srv, officialTool(desc), desc.Handler)
}

// RegisterOfficialCuratedTools exposes only entries selected by allowlist.
// Remaining catalog entries stay behind progressive-disclosure meta-tools.
func RegisterOfficialCuratedTools(srv *mcp.Server, catalog *ToolCatalog, allowed func(string) bool) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	for _, entry := range catalog.Entries() {
		if !allowed(entry.Name) {
			continue
		}
		if err := registerTool(srv, officialTool(toolDescriptor(entry)), entry.Handler); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resource conversion
// ---------------------------------------------------------------------------

// officialResource converts a Pinner-owned resource descriptor into an
// official SDK resource.
func officialResource(desc ResourceDescriptor) *mcp.Resource {
	r := &mcp.Resource{
		URI:         desc.URI,
		Name:        desc.Name,
		Title:       desc.Title,
		Description: desc.Description,
		MIMEType:    desc.MIMEType,
	}
	return r
}

// officialResourceHandler adapts a Pinner-owned resource handler to the
// official SDK handler shape. req.Params.URI carries the concrete URI.
func officialResourceHandler(handler ResourceHandler) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		result, err := handler(ctx, ResourceRequest{
			URI:       req.Params.URI,
			Arguments: map[string]string{},
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: result.URI, MIMEType: result.MIMEType, Text: result.Text},
			},
		}, nil
	}
}

// officialResourceTemplateHandler adapts a Pinner-owned resource-template
// handler. The official SDK resolves the template and passes the concrete URI;
// template variables are not populated automatically, so the handler receives
// the parsed URI variables via Arguments.
func officialResourceTemplateHandler(template string, handler ResourceHandler) mcp.ResourceHandler {
	parsed, err := uritemplate.New(template)
	if err != nil {
		panic(fmt.Sprintf("invalid resource template %q: %v", template, err))
	}
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		arguments := map[string]string{}
		matches := parsed.Regexp().FindStringSubmatch(req.Params.URI)
		if matches != nil {
			for i, name := range parsed.Varnames() {
				if i+1 < len(matches) {
					arguments[name] = matches[i+1]
				}
			}
		}
		result, err := handler(ctx, ResourceRequest{
			URI:       req.Params.URI,
			Arguments: arguments,
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: result.URI, MIMEType: result.MIMEType, Text: result.Text},
			},
		}, nil
	}
}

// RegisterOfficialResources registers static resources and resource templates
// on an official-SDK server.
func RegisterOfficialResources(srv *mcp.Server, resources []ResourceDescriptor, templates []ResourceTemplateDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	for _, r := range resources {
		srv.AddResource(officialResource(r), officialResourceHandler(r.Handler))
	}
	for _, t := range templates {
		srv.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			MIMEType:    t.MIMEType,
		}, officialResourceTemplateHandler(t.URITemplate, t.Handler))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Prompt conversion
// ---------------------------------------------------------------------------

// officialPrompt converts a Pinner-owned prompt descriptor into an official
// SDK prompt.
func officialPrompt(desc PromptDescriptor) *mcp.Prompt {
	p := &mcp.Prompt{
		Name:        desc.Name,
		Title:       desc.Title,
		Description: desc.Description,
	}
	for _, a := range desc.Arguments {
		p.Arguments = append(p.Arguments, &mcp.PromptArgument{
			Name:        a.Name,
			Title:       a.Title,
			Description: a.Description,
			Required:    a.Required,
		})
	}
	return p
}

// officialMessageContent converts a Pinner-owned prompt message into official
// SDK content (text or embedded resource), preserving role and text verbatim.
func officialMessageContent(msg PromptMessage) (mcp.Role, mcp.Content) {
	if msg.EmbeddedResource != nil {
		return mcp.Role(msg.Role), &mcp.EmbeddedResource{
			Resource: &mcp.ResourceContents{
				URI:      msg.EmbeddedResource.URI,
				MIMEType: msg.EmbeddedResource.MIMEType,
				Text:     msg.EmbeddedResource.Text,
			},
		}
	}
	return mcp.Role(msg.Role), &mcp.TextContent{Text: msg.Text}
}

// officialPromptHandler adapts a Pinner-owned prompt handler to the official
// SDK prompt-handler shape. The official SDK delivers arguments as
// map[string]string for downstream command execution.
func officialPromptHandler(handler func(context.Context, PromptRequest) (PromptResult, error)) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		result, err := handler(ctx, PromptRequest{Arguments: req.Params.Arguments})
		if err != nil {
			return nil, err
		}
		out := &mcp.GetPromptResult{Description: result.Description}
		for _, m := range result.Messages {
			role, content := officialMessageContent(m)
			out.Messages = append(out.Messages, &mcp.PromptMessage{Role: role, Content: content})
		}
		return out, nil
	}
}

// RegisterOfficialPrompts registers prompt templates on an official-SDK server.
func RegisterOfficialPrompts(srv *mcp.Server, prompts []PromptDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	for _, p := range prompts {
		srv.AddPrompt(officialPrompt(p), officialPromptHandler(p.Handler))
	}
	return nil
}

// NewStreamableHTTPHandler returns the official SDK streamable-HTTP handler
// bound to the given server. disableLocalhostProtection turns off the go-sdk's
// DNS-rebinding guard, which rejects requests arriving via a loopback local
// address that carry a non-loopback Host header (403 "invalid Host header").
// This is required when the server listens on 127.0.0.1 but is reached through
// a public tunnel (remote clients send the tunnel's hostname as the Host
// header); it must be kept false when serving only on the loopback directly.
func NewStreamableHTTPHandler(getServer func(*http.Request) *mcp.Server, disableLocalhostProtection bool) http.Handler {
	var opts *mcp.StreamableHTTPOptions
	if disableLocalhostProtection {
		opts = &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true}
	}
	return mcp.NewStreamableHTTPHandler(getServer, opts)
}

// NewOfficialStreamableHandler builds the official streamable-HTTP handler for
// an OfficialServer. This is what the shared serving path uses so it can stay
// SDK-neutral.
func NewOfficialStreamableHandler(srv *OfficialServer, disableLocalhostProtection bool) http.Handler {
	return NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, disableLocalhostProtection)
}
