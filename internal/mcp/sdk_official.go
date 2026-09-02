// Official MCP SDK adapter.
//
// This file is the hub-side adapter that speaks the sdk seam: it converts
// Pinner-owned descriptors (defined in protocol_model.go) and handlers into
// registrations on the official MCP server, preserving Pinner's wire JSON
// contract exactly:
//
//   - the visible meta-tools (search_tools, describe_tool, and the typed
//     invoke dispatchers invoke_read_tool / invoke_write_tool /
//     invoke_destructive_tool) and their serialized schemas;
//   - the progressive-disclosure catalog invocation behavior;
//   - pinner:// resource and resource-template URIs, MIME types and payloads;
//   - prompt names, arguments, roles, text and embedded resources.
//
// All go-sdk types are accessed exclusively through the sdk package, which is
// the only production package that imports the protocol SDK's mcp types; this
// file imports no SDK package directly. The catalog, wizard, resource-provider,
// prompt and OAuth domain logic speak Pinner-owned descriptors and handlers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
)

// transportFlags carries the server launch configuration that the detector
// needs to resolve the correct platform profile. It is set once at server
// startup from CLI flags via SetTransportFlags.
type transportFlags struct {
	coLocated    bool
	tunnelOpenAI bool
}

// transportFlagsVar is the package-level transport flags read by requestCaps.
// It is set once at server startup (SetTransportFlags) before any tools are
// registered or requests served.
var transportFlagsVar = transportFlags{}

// SetTransportFlags sets the server launch transport configuration so that
// requestCaps can resolve the correct platform profile for each request.
func SetTransportFlags(coLocated, tunnelOpenAI bool) {
	transportFlagsVar = transportFlags{coLocated: coLocated, tunnelOpenAI: tunnelOpenAI}
}

// devToolsEnabled records whether the MCP server was launched with --dev-tools.
// When enabled, requestCaps additionally captures the raw wire snapshot
// (client capabilities + initialize params) on every request so the dev_* tools
// can introspect the connected host. When disabled the raw snapshot is omitted,
// keeping the hot path lean.
var devToolsEnabled bool

// SetDevTools enables or disables the per-request raw wire snapshot that back
// the dev_* introspection tools. It must be called once at server startup, from
// the same place SetTransportFlags is called.
func SetDevTools(enabled bool) {
	devToolsEnabled = enabled
}

// defaultDetectorRegistry is the registry used by requestCaps to resolve
// the connected platform from wire signals. It is package-scoped so it can
// be overridden in tests.
var defaultDetectorRegistry = hostenv.NewRegistry()

// sdkHandlerDeps is the hub's implementation of the behaviors the sdk handler
// adapter needs: per-request capabilities, request-state echo key, operation
// logging, and companion-app annotation on needs_human results.
var sdkHandlerDeps = sdk.HandlerDeps{
	RequestCaps: func(req *sdk.CallToolRequest) *model.RequestCaps {
		return requestCaps(req, transportFlagsVar)
	},
	ReservedRequestStateKey: catalog.ReservedRequestStateKey,
	LogStart:                func(name string, args map[string]any) { logToolCallStart(log, name, args) },
	LogEnd: func(name string, startedAt time.Time, result model.ToolResult, err error) {
		logToolCallEnd(log, name, startedAt, result, err)
	},
	AnnotateApp: annotateAppOnHandoff,
}

// OfficialServerFromCatalog builds the official server with Pinner's
// progressive-disclosure meta-tools. The catalog remains internal.
func OfficialServerFromCatalog(catalog *ToolCatalog, instructions string, stdioMode bool, seedDrop *oob.SeedDrop, oobRestore *oob.OOBRestore, oobCreate *oob.OOBCreate) (*sdk.Server, error) {
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
func OfficialMCPServer(root *cli.Command, stdioMode bool, seedDrop *oob.SeedDrop, oobRestore *oob.OOBRestore, oobCreate *oob.OOBCreate, handoffReg *handoff.HandoffRegistry, authHandles *session.AsyncHandleStore, catalogOpts ...buildCatalogOpt) (*sdk.Server, *ToolCatalog, error) {
	catalog, err := buildCatalog(root, seedDrop, oobRestore, oobCreate, handoffReg, authHandles, catalogOpts...)
	if err != nil {
		return nil, nil, err
	}
	srv, err := OfficialServerFromCatalog(catalog, buildInstructions(catalog.Len()), stdioMode, seedDrop, oobRestore, oobCreate)
	if err != nil {
		return nil, nil, err
	}
	return srv, catalog, nil
}

// requestCaps builds the SDK-neutral per-request capability view of the
// calling client from an official SDK call-tool request. MCP is stateless: the
// capabilities arrive in the request _meta (with a legacy initialize-handshake
// fallback), so this is re-derived for every invocation rather than stored on
// a session.
//
// In addition to the legacy fields (ProtocolVersion, ClientName, UI), this now
// also reads req.GetExtra() to extract HTTP headers (User-Agent) and OAuth
// TokenInfo — signals the go-sdk carries but Pinner previously ignored. These
// feed the hostenv DetectorRegistry to produce a PlatformProfile.
//
// transportFlags carries the server launch configuration (co-located stdio
// vs OpenAI tunnel vs HTTP), which the detector needs to resolve the correct
// platform profile. It is threaded from the handler deps rather than captured
// in a closure to keep requestCaps a pure function of its inputs.
func requestCaps(req *sdk.CallToolRequest, transportFlags transportFlags) *model.RequestCaps {
	rc := &model.RequestCaps{ProtocolVersion: req.ProtocolVersion()}
	if ci := req.ClientInfo(); ci != nil {
		rc.ClientName = ci.Name
		rc.ClientVersion = ci.Version
	}
	if cc := req.ClientCapabilities(); cc != nil {
		rc.UI = apps.GetClientUICapability(cc.Extensions)
	}

	// Extract wire signals from req.GetExtra() — the go-sdk carries HTTP
	// headers and OAuth TokenInfo here, but only over HTTP transports.
	// On stdio, Extra is nil.
	var headers http.Header
	var tokenInfo *hostenv.TokenInfo
	if extra := req.GetExtra(); extra != nil {
		headers = extra.Header
		if extra.TokenInfo != nil {
			tokenInfo = &hostenv.TokenInfo{
				Scopes:     extra.TokenInfo.Scopes,
				Expiration: extra.TokenInfo.Expiration,
				UserID:     extra.TokenInfo.UserID,
				Extra:      extra.TokenInfo.Extra,
			}
		}
	}

	// Build the DetectRequest from all available wire signals and resolve
	// a PlatformProfile. The profile carries host type, transport, features,
	// and raw signals — tools read it via request.Caps.Profile.
	var ci *hostenv.ClientInfo
	if req.ClientInfo() != nil {
		wireCI := req.ClientInfo()
		ci = &hostenv.ClientInfo{
			Name:        wireCI.Name,
			Version:     wireCI.Version,
			Title:       wireCI.Title,
			Description: wireCI.Description,
		}
	}

	var userAgent string
	if headers != nil {
		userAgent = headers.Get("User-Agent")
	}

	profile := defaultDetectorRegistry.Detect(hostenv.DetectRequest{
		ClientInfo:      ci,
		ProtocolVersion: req.ProtocolVersion(),
		UserAgent:       userAgent,
		Headers:         headers,
		TokenInfo:       tokenInfo,
		CoLocated:       transportFlags.coLocated,
		TunnelOpenAI:    transportFlags.tunnelOpenAI,
	})

	// Safety net: if the client advertises MCP Apps support on the wire
	// (io.modelcontextprotocol/ui extension with text/html;profile=mcp-app)
	// but the resolved static profile doesn't declare FeatMCPApps, overlay
	// it so tools can branch at call time. This covers future hosts that
	// advertise the capability but have no static profile entry yet.
	if rc.UI != nil && rc.UI.SupportsApps() && !profile.Features[hostenv.FeatMCPApps] {
		profile = profile.CloneFeatures()
		profile.Features[hostenv.FeatMCPApps] = true
	}

	// When dev tools are enabled, capture the raw wire snapshot the dev_*
	// tools introspect. The go-sdk types are converted to SDK-neutral JSON
	// data so the model layer stays free of the protocol SDK. This is the
	// only signal that reliably describes a remote host across HTTP/OAuth
	// transports (the server's own process environment is unrelated).
	if devToolsEnabled {
		if cc := req.ClientCapabilities(); cc != nil {
			rc.Capabilities = toJSONMap(cc)
		}
		if s := req.Session; s != nil {
			if ip := s.InitializeParams(); ip != nil {
				rc.InitializeParams = toJSONMap(ip)
			}
		}
	}

	rc.Profile = &profile

	return rc
}

// toJSONMap converts a go-sdk-typed value into a plain JSON map so the
// SDK-neutral model layer can carry it without importing the protocol SDK. It
// returns nil when the value cannot be marshaled (defensive; the SDK structs
// used here always serialize).
func toJSONMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
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
	app, ok := apps.AppInfoForTool(toolName)
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

// registerTool is the hub's app-tool registration seam (installed via
// sdk.SetToolRegistrar in adapter.go). It routes app-tool registration through
// the same handler-adaptation deps as the meta-tools, so app tools attached to
// a ui:// view reuse the single registration path.
func registerTool(srv *sdk.Server, desc model.ToolDescriptor, handler model.PinnerToolHandler) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	desc.Handler = handler
	return sdk.RegisterTool(srv, sdkHandlerDeps, desc)
}

// RegisterOfficialMetaTools registers the progressive-disclosure meta-tools
// (search_tools, describe_tool, and the typed invoke dispatchers
// invoke_read_tool / invoke_write_tool / invoke_destructive_tool) on an
// official-SDK server. The catalog itself stays hidden; the tools visible via
// tools/list are these five, preserving progressive disclosure while keeping
// each invoke tool's hints truthful about the safety class it executes.
func RegisterOfficialMetaTools(srv *sdk.Server, catalog *ToolCatalog, stdioMode bool, seedDrop *oob.SeedDrop, oobRestore *oob.OOBRestore, oobCreate *oob.OOBCreate) error {
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
	return registerOfficialInvokeTools(srv, catalog, stdioMode, seedDrop, oobRestore, oobCreate)
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

// invokeToolInput is the typed argument shape for the typed invoke
// dispatchers (invoke_read_tool / invoke_write_tool / invoke_destructive_tool).
type invokeToolInput struct {
	Name      string         `json:"name" jsonschema:"description=Tool name from search_tools result."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"description=Arguments object matching the tool's inputSchema."`
}

func registerOfficialSearchTools(srv *sdk.Server, catalog *ToolCatalog) error {
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
	discoveryNote := "Search the internal tool catalog by a single keyword. No boolean (AND/OR) syntax: pass one keyword at a time (e.g. 'pin', not 'pin OR upload'). Name matches are ranked exact, then starts-with, contains, then within-segment subsequence (a fuzzy abbreviation within a single word of the name), then whole-word description matches; tools that never match are omitted. Use the 'category' filter to narrow scope and 'limit' to cap results. Leave query empty or use 'help' for an onboarding listing of just the primary start-here tools, which also carries agent_guide for the full flows and a hint pointing at category browsing for a specific domain. Workflow: after discovering a tool here, call describe_tool(name) for its input schema; the describe response carries an invokeTool field naming the typed dispatcher that executes it (invoke_read_tool for read-only tools, invoke_write_tool for mutating tools, invoke_destructive_tool for destructive tools — each dispatcher refuses out-of-class tools, so route by the named one). Capability and file-transfer tools are exposed directly on the tool surface AND indexed here, so they are discoverable by name (e.g. 'upload', 'capabilities'). Interactive wizard flows (category 'wizard') are excluded unless you filter for them specifically."

	desc := model.ToolDescriptor{
		Name:          "search_tools",
		Title:         "Search tool catalog",
		Description:   discoveryNote,
		OpenWorldHint: false,
		InputSchema:   schema.raw(),
	}

	desc.Handler = model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
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
			tools := catalog.SearchFor(in.Query, in.Category, in.Limit, profileFromRequest(request))
			data, err = json.Marshal(SearchResult{Tools: tools, Total: len(tools)})
		}
		if err != nil {
			return model.ToolResult{}, err
		}
		return model.ToolResult{Text: string(data)}, nil
	})

	return sdk.RegisterTool(srv, sdkHandlerDeps, desc)
}

func registerOfficialDescribeTool(srv *sdk.Server, catalog *ToolCatalog) error {
	schema := &metaToolSchema{}
	schema.property("name", map[string]any{
		"type":        "string",
		"description": "Tool name from search_tools result",
	})

	desc := model.ToolDescriptor{
		Name:          "describe_tool",
		Title:         "Describe a catalog tool",
		Description:   "Get the full input schema for a single tool by name. Use the tool name returned by search_tools. The inputSchema field contains the JSON Schema that the tool's arguments conform to.",
		OpenWorldHint: false,
		InputSchema:   schema.raw(),
	}

	desc.Handler = model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[describeToolInput](request)
		if err != nil {
			return model.ToolResult{IsError: true, Text: err.Error()}, nil
		}
		if in.Name == "" {
			return model.ToolResult{IsError: true, Text: "name is required"}, nil
		}
		detail, err := catalog.DescribeFor(in.Name, profileFromRequest(request))
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

	return sdk.RegisterTool(srv, sdkHandlerDeps, desc)
}

// invokeClass identifies the safety class a typed invoke dispatcher admits.
// The platform directory validators (OpenAI/Claude) require a tool's hints to
// match what the tool CAN do, and explicitly reject a single catch-all tool
// that mixes safe and unsafe operations. name+arguments dispatch therefore
// lives in three typed tools, each admitted exactly one safety class, so the
// wire annotations are truthful and no dispatcher straddles a safety
// boundary.
type invokeClass int

const (
	invokeClassRead invokeClass = iota
	invokeClassWrite
	invokeClassDestructive
)

// classifyEntry maps a catalog entry's platform hints onto the typed-invoke
// safety class. The hint values (not the raw catalog Safety) drive the split
// because they are the platform-truthful classification — e.g. the auth_status
// override (an out-of-band sign-in email cannot be unsent) moves it into the
// destructive bucket despite its SafetyRead origin. A readOnly entry that
// still declares openWorld contradicts the read contract (validators reject
// readOnly+openWorld), so it is conservatively reachable only through the
// write dispatcher, whose hints cover it.
func classifyEntry(entry *model.ToolEntry) invokeClass {
	switch {
	case entry.Destructive:
		return invokeClassDestructive
	case entry.ReadOnly && !entry.OpenWorldHint:
		return invokeClassRead
	default:
		return invokeClassWrite
	}
}

// dispatcher is the MCP tool name of the typed invoke tool for this class.
func (c invokeClass) dispatcher() string {
	switch c {
	case invokeClassRead:
		return "invoke_read_tool"
	case invokeClassDestructive:
		return "invoke_destructive_tool"
	default:
		return "invoke_write_tool"
	}
}

// registerOfficialInvokeTools registers the three typed invoke dispatchers.
// Each executes only catalog tools of its own safety class and refuses the
// rest with a pointer to the right dispatcher, keeping progressive discovery
// while making every MCP tool's annotations match its real capabilities.
func registerOfficialInvokeTools(srv *sdk.Server, catalog *ToolCatalog, stdioMode bool, seedDrop *oob.SeedDrop, oobRestore *oob.OOBRestore, oobCreate *oob.OOBCreate) error {
	specs := []struct {
		name        string
		title       string
		description string
		class       invokeClass
		readOnly    bool
		destructive bool
		openWorld   bool
	}{
		{
			name:        "invoke_read_tool",
			title:       "Invoke a read-only catalog tool",
			description: "Execute a read-only catalog tool by name with the given arguments. This is the third step of the discovery workflow: search_tools(name) to find a tool, describe_tool(name) for its input schema (the describe response names the dispatcher to invoke), then invoke_read_tool(name, arguments) when the tool's hints are readOnlyHint=true. The dispatcher refuses non-read-only tools; use invoke_write_tool or invoke_destructive_tool for those. The arguments object is validated against the tool's inputSchema returned by describe_tool.",
			class:       invokeClassRead,
			readOnly:    true,
		},
		{
			name:        "invoke_write_tool",
			title:       "Invoke a mutating catalog tool",
			description: "Execute a state-mutating (but not destructive) catalog tool by name with the given arguments. This is the third step of the discovery workflow: search_tools(name) to find a tool, describe_tool(name) for its input schema (the describe response names the dispatcher to invoke), then invoke_write_tool(name, arguments) when the tool's hints are readOnlyHint=false and destructiveHint=false. The dispatcher refuses read-only and destructive tools; use invoke_read_tool or invoke_destructive_tool for those. The arguments object is validated against the tool's inputSchema returned by describe_tool.",
			class:       invokeClassWrite,
			openWorld:   true,
		},
		{
			name:        "invoke_destructive_tool",
			title:       "Invoke a destructive catalog tool",
			description: "Execute a destructive (irreversible / deletion) catalog tool by name with the given arguments. This is the third step of the discovery workflow: search_tools(name) to find a tool, describe_tool(name) for its input schema (the describe response names the dispatcher to invoke), then invoke_destructive_tool(name, arguments) when the tool's hints carry destructiveHint=true. Destructive operations additionally require human confirmation (the server returns a needs_human hand-off before running). The dispatcher refuses non-destructive tools; use invoke_read_tool or invoke_write_tool for those. The arguments object is validated against the tool's inputSchema returned by describe_tool.",
			class:       invokeClassDestructive,
			destructive: true,
			openWorld:   true,
		},
	}

	for _, spec := range specs {
		schema := &metaToolSchema{}
		schema.property("name", map[string]any{
			"type":        "string",
			"description": "Tool name from search_tools result",
		})
		schema.property("arguments", map[string]any{
			"type":        "object",
			"description": "Arguments object matching the tool's inputSchema. Use describe_tool to see the schema.",
		})

		desc := model.ToolDescriptor{
			Name:          spec.name,
			Title:         spec.title,
			Description:   spec.description,
			ReadOnly:      spec.readOnly,
			Destructive:   spec.destructive,
			OpenWorldHint: spec.openWorld,
			InputSchema:   schema.raw(),
		}

		desc.Handler = model.PinnerToolHandler(func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
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

			// Safety-class gate: each dispatcher admits one class only. This is
			// the server-side half of the typed-invoke split — the annotations
			// claim a capability, and the handler enforces that the capability
			// boundary actually holds at dispatch time.
			if got := classifyEntry(entry); got != spec.class {
				return model.ToolResult{IsError: true, Text: fmt.Sprintf("tool %s is a %s operation; call %s(name, arguments) instead", in.Name, classNoun(got), got.dispatcher())}, nil
			}

			// Admin tools are gated from the invoke dispatchers, matching the
			// search/describe gate: an agent that somehow knows an admin tool
			// name by heart cannot invoke it. Admin tools are only discoverable
			// via search_tools(category=admin) and are not invokable through
			// this path.
			if entry.Category == model.CategoryAdmin {
				return model.ToolResult{IsError: true, Text: fmt.Sprintf("admin tool %s is not available through the invoke dispatchers; use search_tools with category=admin to discover admin tools", in.Name)}, nil
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

			// Thread the calling client's Caps through to the inner tool so it can
			// adapt per host (e.g. profile-aware dev_* tools). Caps was previously
			// dropped here; every handler already nil-guards it.
			result, err := entry.Handler(ctx, model.ToolRequest{Name: in.Name, Arguments: toolArgs, Caps: request.Caps})
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}
			// The typed dispatcher routes to the inner catalog handler directly,
			// so the outer adapter's annotation (keyed on the dispatcher name)
			// never sees the real tool. Annotate here with the resolved inner
			// name so companion-app metadata reaches text-only hosts for
			// non-DirectVisible tools (e.g. vault_create/vault_restore) that are
			// only reachable through the meta-tools.
			annotateAppOnHandoff(in.Name, request.Caps, &result)
			return result, nil
		})

		if err := sdk.RegisterTool(srv, sdkHandlerDeps, desc); err != nil {
			return err
		}
	}
	return nil
}

// classNoun names a safety class in human-readable error text.
func classNoun(c invokeClass) string {
	switch c {
	case invokeClassRead:
		return "read-only"
	case invokeClassDestructive:
		return "destructive"
	default:
		return "mutating"
	}
}

// RegisterOfficialDescriptor adds one Pinner-owned tool directly to tools/list.
func RegisterOfficialDescriptor(srv *sdk.Server, desc model.ToolDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if desc.Name == "" || desc.Handler == nil {
		return fmt.Errorf("direct tool requires name and handler")
	}
	return sdk.RegisterTool(srv, sdkHandlerDeps, desc)
}

// RegisterOfficialCuratedTools exposes the catalog's directly-visible tools
// (those with DirectVisible set) as standard tools/list tools. Remaining
// catalog entries stay behind the progressive-disclosure meta-tools
// (search_tools / describe_tool / invoke_read_tool / invoke_write_tool /
// invoke_destructive_tool) which index the whole catalog.
func RegisterOfficialCuratedTools(srv *sdk.Server, catalog *ToolCatalog) error {
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
		desc := model.ToolDescriptorFromEntry(entry)
		desc.Handler = entry.Handler
		if err := sdk.RegisterTool(srv, sdkHandlerDeps, desc); err != nil {
			return err
		}
	}
	return nil
}
