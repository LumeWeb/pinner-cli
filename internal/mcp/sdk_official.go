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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// build.Version is stamped by ldflags. Fall back to a dev constant when it is
// empty (e.g. during `go test`).
const officialSDKVersion = "v1.4.1"

// OfficialServerFromCatalog builds the official server with Pinner's
// progressive-disclosure meta-tools. The catalog remains internal.
func OfficialServerFromCatalog(catalog *ToolCatalog, instructions string, stdioMode bool, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate) (*mcp.Server, error) {
	if catalog == nil {
		return nil, fmt.Errorf("nil tool catalog")
	}
	srv := sdk.NewServer(&sdk.ServerOptions{Instructions: instructions})
	if err := RegisterOfficialMetaTools(srv, catalog, stdioMode, seedDrop, oobRestore, oobCreate); err != nil {
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
func OfficialMCPServer(root *cli.Command, hasRootAction bool, prefix []string, stdioMode bool, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate, handoffReg *handoff.HandoffRegistry, authHandles *session.AsyncHandleStore, catalogOpts ...buildCatalogOpt) (*mcp.Server, *ToolCatalog, error) {
	catalog, err := buildCatalog(root, hasRootAction, prefix, seedDrop, oobRestore, oobCreate, handoffReg, authHandles, catalogOpts...)
	if err != nil {
		return nil, nil, err
	}
	srv, err := OfficialServerFromCatalog(catalog, buildInstructions(catalog.Len()), stdioMode, seedDrop, oobRestore, oobCreate)
	if err != nil {
		return nil, nil, err
	}
	return srv, catalog, nil
}

// PinnerToolHandler → mcp.ToolHandler. The arguments arrive on the wire as
// raw JSON; unmarshal them into a plain map for the Pinner-owned handler. When
// the call is a retry after an input_required elicitation, the accepted form
// content is merged into the arguments under their elicitation id so handlers
// read form submissions like any other argument.
func officialToolHandler(handler model.PinnerToolHandler) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if req.Params.Arguments != nil {
			// Decode with UseNumber so JSON integers arrive as json.Number
			// instead of float64. Plain json.Unmarshal maps an integer to
			// float64, which silently loses precision for any value above
			// 2^53; for an id like ipns_keys_get/delete's that could address
			// (or with delete, remove) the wrong key. json.Number is exact,
			// and the catalog normalizer converts it losslessly.
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.UseNumber()
			if err := dec.Decode(&args); err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid arguments: %v", err)}},
				}, nil
			}
		}
		for id, content := range sdk.AcceptedElicitations(req) {
			args[id] = content
		}
		// Recover cross-round state the client echoed back on a retry after an
		// input_required result, so handlers can re-establish context (e.g. a
		// session id) even if the client did not echo the original arguments.
		if req.Params.RequestState != "" {
			if _, ok := args[catalog.ReservedRequestStateKey]; !ok {
				args[catalog.ReservedRequestStateKey] = req.Params.RequestState
			}
		}
		startedAt := time.Now()
		logToolCallStart(log, req.Params.Name, args)
		result, err := handler(ctx, model.ToolRequest{
			Name:           req.Params.Name,
			Arguments:      args,
			InputResponses: len(req.Params.InputResponses) > 0,
			Caps:           requestCaps(req),
		})
		logToolCallEnd(log, req.Params.Name, startedAt, result, err)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}
		annotateAppOnHandoff(req.Params.Name, requestCaps(req), &result)
		return officialToolResult(result), nil
	}
}

// annotateAppOnHandoff appends companion-app context to a needs_human tool
// result for a model-visible tool that has an attached MCP App. Per the MCP
// Apps spec, the app chrome lives on the tool metadata (ui:// resource) and is
// fetched by UI-capable hosts; the model always reads content[].text. Without
// an annotation, a text-only host (e.g. a plain MCP bridge) cannot tell the
// user that an interactive page exists alongside the raw URL/handle flow. We
// therefore surface the companion app in the text (and, when the calling
// client supports MCP Apps, we say the page renders inline). This is additive:
// non-app tools and non-needs_human results pass through unchanged.
func annotateAppOnHandoff(toolName string, caps *model.RequestCaps, result *model.ToolResult) {
	if result == nil || result.IsError || result.Elicitation != nil {
		return
	}
	app, ok := appInfoForTool(toolName)
	if !ok {
		return
	}
	sc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return
	}
	if status, _ := sc["status"].(string); status != model.StatusNeedsHuman {
		return
	}
	if caps != nil && caps.SupportsApps() {
		result.Text += " A companion interactive page (\"" + app.Title + "\") will render in your client for this step."
	} else {
		result.Text += " A companion interactive page (" + app.Title + "; " + app.URI + ") is also available in Apps-capable clients; the URL above is the direct fallback."
	}
	// Mirror the app reference into structured content for clients that
	// consume structuredContent as well as text.
	sc["app"] = map[string]any{
		"uri": app.URI, "name": app.Name, "title": app.Title,
	}
}

// requestCaps builds the SDK-neutral per-request capability view of the
// calling client from an official SDK call-tool request. MCP is stateless: the
// capabilities arrive in the request _meta (with a legacy initialize-handshake
// fallback), so this is re-derived for every invocation rather than stored on
// a session.
func requestCaps(req *mcp.CallToolRequest) *model.RequestCaps {
	rc := &model.RequestCaps{ProtocolVersion: req.ProtocolVersion()}
	if ci := req.ClientInfo(); ci != nil {
		rc.ClientName = ci.Name
		rc.ClientVersion = ci.Version
	}
	if cc := req.ClientCapabilities(); cc != nil {
		rc.UI = GetClientUICapability(cc.Extensions)
	}
	return rc
}

// officialToolResult converts a Pinner-owned tool result into an official SDK
// result, preserving isError and single text-content semantics. When the result
// carries an Elicitation it is converted into an input_required response.
func officialToolResult(result model.ToolResult) *mcp.CallToolResult {
	if result.Elicitation != nil {
		return sdk.CallToolResultFromElicitation(*result.Elicitation)
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
func registerTool(srv *mcp.Server, tool *mcp.Tool, handler model.PinnerToolHandler) error {
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
func RegisterOfficialMetaTools(srv *mcp.Server, catalog *ToolCatalog, stdioMode bool, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate) error {
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
	return registerOfficialInvokeTool(srv, catalog, stdioMode, seedDrop, oobRestore, oobCreate)
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
	Query    string `json:"query,omitempty" jsonschema:"description=A single keyword to search for in tool names and descriptions."`
	Category string `json:"category,omitempty" jsonschema:"description=Filter by category: core, account, vault, ipns, operations, admin, or wizard."`
	// Limit caps the number of results returned. Leave unset/0 for no cap.
	Limit int `json:"limit,omitempty" jsonschema:"description=Optional maximum number of results to return. Leave unset for no limit."`
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
		"description": "A single keyword to search for in tool names (and, as a whole word only, descriptions). Name matches rank above description matches, so e.g. 'auth' finds the auth_* tools, not every tool whose description happens to contain a word starting with auth. Leave empty (or use 'help') for an onboarding listing of just the primary start-here tools (auth/vault/pins flows), with a hint pointing at agent_guide and category browsing.",
	})
	schema.property("category", map[string]any{
		"type":        "string",
		"description": "Filter by category: 'core' (user commands incl. pins/dns/websites), 'account' (auth, api keys), 'vault', 'ipns', 'operations', 'admin', or 'wizard'. Wizards are hidden from general search unless you set category to 'wizard' explicitly. Leave empty to search all categories.",
	})
	schema.property("limit", map[string]any{
		"type":        "integer",
		"description": "Optional maximum number of results to return. Leave unset for no limit.",
	})

	// Discovery workflow. This description documents the full search ->
	// describe -> invoke loop and the dual-surface policy (some file-I/O
	// tools are host-curated and not in this catalog).
	discoveryNote := "Search the internal tool catalog by a single keyword. No boolean (AND/OR) syntax: pass one keyword at a time (e.g. 'pin', not 'pin OR upload'). Name matches are ranked exact, then starts-with, contains, then within-segment subsequence (a fuzzy abbreviation within a single word of the name), then whole-word description matches; tools that never match are omitted. Use the 'category' filter to narrow scope and 'limit' to cap results. Leave query empty or use 'help' for an onboarding listing of just the primary start-here tools, which also carries a hint pointing at agent_guide for the full flows and at category browsing for a specific domain. Workflow: after discovering a tool here, call describe_tool(name) for its input schema, then invoke_tool(name, arguments). File upload and capability tools (upload_data, upload_url, capabilities) are host-curated and not listed in this catalog; they are exposed directly on the tool surface. Interactive wizard flows (category 'wizard') are excluded unless you filter for them specifically."

	tool := &mcp.Tool{
		Name:        "search_tools",
		Description: discoveryNote,
		InputSchema: schema.raw(),
	}

	handler := model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[searchToolsInput](request)
		if err != nil {
			return model.ToolResult{}, err
		}

		// Route between the two discovery surfaces. Pure onboarding (empty/help
		// query, no category) returns the curated primary start-here tools plus
		// a pointer onward; anything else is a keyword search (an empty query
		// with an explicit category browses that whole category).
		var data []byte
		if isOnboardingQuery(strings.ToLower(strings.TrimSpace(in.Query))) && in.Category == "" {
			res := catalog.Onboarding()
			res.Hint = "These are the primary start-here tools for the four flows (auth, vault_create, vault_restore, pins). Call agent_guide for the full ordered chains, or search with category=core|account|vault|ipns|operations|admin (or category=wizard for wizards) to browse a specific domain."
			// Honor the documented limit contract on the onboarding path too:
			// cap the result set to in.Limit when it is > 0.
			if in.Limit > 0 && len(res.Tools) > in.Limit {
				res.Tools = res.Tools[:in.Limit]
				res.Total = len(res.Tools)
			}
			data, err = json.Marshal(res)
		} else {
			tools := catalog.Search(in.Query, in.Category, in.Limit)
			data, err = json.Marshal(SearchResult{Tools: tools, Total: len(tools)})
		}
		if err != nil {
			return model.ToolResult{}, err
		}
		return model.ToolResult{Text: string(data)}, nil
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

	handler := model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[describeToolInput](request)
		if err != nil {
			return model.ToolResult{IsError: true, Text: err.Error()}, nil
		}
		if in.Name == "" {
			return model.ToolResult{IsError: true, Text: "name is required"}, nil
		}
		detail, err := catalog.Describe(in.Name)
		if err != nil {
			// Unknown tool: answer with "did you mean ...?" so the agent can
			// recover without a separate search round-trip.
			suggestions := catalog.Suggest(in.Name, 3)
			resp := map[string]any{
				"error":   err.Error(),
				"suggest": suggestions,
			}
			if len(suggestions) > 0 {
				resp["message"] = "unknown tool. did you mean one of these?"
			}
			out, _ := json.Marshal(resp)
			return model.ToolResult{IsError: true, Text: string(out)}, nil
		}
		data, err := json.Marshal(detail)
		if err != nil {
			return model.ToolResult{}, err
		}
		return model.ToolResult{Text: string(data)}, nil
	})

	return registerTool(srv, tool, handler)
}

func registerOfficialInvokeTool(srv *mcp.Server, catalog *ToolCatalog, stdioMode bool, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate) error {
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
		Description: "Execute a tool by name with the given arguments. This is the third step of the discovery workflow: search_tools(name) to find a tool, describe_tool(name) for its input schema, then invoke_tool(name, arguments). The arguments object must match the tool's inputSchema returned by describe_tool.",
		InputSchema: schema.raw(),
	}

	handler := model.PinnerToolHandler(func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[invokeToolInput](request)
		if err != nil {
			return model.ToolResult{IsError: true, Text: err.Error()}, nil
		}
		if in.Name == "" {
			return model.ToolResult{IsError: true, Text: "name is required"}, nil
		}
		toolArgs := in.Arguments
		if toolArgs == nil {
			toolArgs = map[string]any{}
		}
		entry, ok := catalog.Get(in.Name)
		if !ok {
			// Unknown tool: offer nearest names so the agent can recover
			// without a separate search round-trip.
			suggestions := catalog.Suggest(in.Name, 3)
			resp := map[string]any{
				"error":   fmt.Sprintf("unknown tool: %s", in.Name),
				"suggest": suggestions,
			}
			if len(suggestions) > 0 {
				resp["message"] = "unknown tool. did you mean one of these?"
			}
			out, _ := json.Marshal(resp)
			return model.ToolResult{IsError: true, Text: string(out)}, nil
		}

		// Steer agents away from commands they cannot run safely over the MCP
		// channel, instead of letting them hang. A human-only (interactive)
		// command always redirects. Everything else runs normally.
		//
		// Stdin-reading is a CLI-side concern only and is never gated here: a
		// command whose action reads piped stdin (e.g. `vault restore
		// --seed-stdin`) is a human/terminal mechanism that is not exposed
		// through MCP. The agent-facing vault tools are the agent-safe OOB
		// hand-offs (vaultSetupOps), which never touch os.Stdin. So the invoke
		// gate only redirects interactive (human-only setup) tools.
		switch entry.Interaction {
		case model.InteractionInteractive:
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonInteractiveOnly,
				ResumeTool: "",
				Detail:     "This command is human-only (it prompts interactively) and has no agent-safe form. Run it via the CLI, or use the curated agent tool for the same workflow.",
			}), nil
		}

		result, err := entry.Handler(ctx, model.ToolRequest{Name: in.Name, Arguments: toolArgs})
		if err != nil {
			return model.ToolResult{IsError: true, Text: err.Error()}, nil
		}
		// invoke_tool dispatches to the inner catalog handler directly, so the
		// outer officialToolHandler's annotation (keyed on req.Params.Name ==
		// "invoke_tool") never sees the real tool. Annotate here with the
		// resolved inner name so companion-app metadata reaches text-only hosts
		// for non-DirectVisible tools (e.g. vault_create/vault_restore) that are
		// only reachable through this meta-tool.
		annotateAppOnHandoff(in.Name, request.Caps, &result)
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
	return RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil)
}

// RegisterOfficialDescriptor adds one Pinner-owned tool directly to tools/list.
func RegisterOfficialDescriptor(srv *mcp.Server, desc model.ToolDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if desc.Name == "" || desc.Handler == nil {
		return fmt.Errorf("direct tool requires name and handler")
	}
	return registerTool(srv, sdk.Tool(desc), desc.Handler)
}

// RegisterOfficialCuratedTools exposes the catalog's directly-visible tools
// (those with DirectVisible set) as standard tools/list tools. Remaining
// catalog entries stay behind the progressive-disclosure meta-tools
// (search_tools / describe_tool / invoke_tool) which index the whole catalog.
func RegisterOfficialCuratedTools(srv *mcp.Server, catalog *ToolCatalog) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	for _, entry := range catalog.Entries() {
		if !entry.DirectVisible {
			continue
		}
		if err := registerTool(srv, sdk.Tool(model.ToolDescriptorFromEntry(entry)), entry.Handler); err != nil {
			return err
		}
	}
	return nil
}
