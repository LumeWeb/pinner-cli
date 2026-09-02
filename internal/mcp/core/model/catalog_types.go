package model

import "encoding/json"

// ToolCategory classifies a tool for filtering during discovery.
type ToolCategory string

const (
	CategoryCore       ToolCategory = "core"
	CategoryAccount    ToolCategory = "account"
	CategoryVault      ToolCategory = "vault"
	CategoryIPNS       ToolCategory = "ipns"
	CategoryOperations ToolCategory = "operations"
	CategoryAdmin      ToolCategory = "admin"
	CategoryWizard     ToolCategory = "wizard"
)

// Interaction classifies how a tool behaves when invoked by an agent over the
// MCP channel (via the typed invoke dispatchers). It lets the server steer agents away from
// commands that would read drained stdin or block on a prompt, and instead
// return a structured redirect so an agent never hangs on a deep command.
//
// The classification comes from the compiler-backed operation surface and is
// stamped on each ToolEntry at registration time.
type Interaction string

const (
	// InteractionAgentSafe marks a tool that is non-blocking for agents: it
	// either completes, fast-fails, or returns a needs_human redirect. This is
	// the default.
	InteractionAgentSafe Interaction = "agent_safe"
	// InteractionInteractive marks a tool that is purely human-facing (a
	// wizard/setup flow that prompts interactively). Agents should not invoke
	// it; the invoke dispatchers redirect, and search_tools hides it.
	InteractionInteractive Interaction = "interactive"
)

// ToolEntry is a single tool in the internal catalog. It stores everything
// the meta-tools need to describe and invoke a tool without exposing it
// via the standard tools/list endpoint.
type ToolEntry struct {
	Name        string
	Title       string
	Description string
	Category    ToolCategory
	ReadOnly    bool
	Destructive bool
	// OpenWorldHint declares whether the tool may interact with the
	// "open world" of external systems; carried through to the wire
	// annotations (see ToolDescriptor.OpenWorldHint).
	OpenWorldHint bool
	// DirectVisible reports whether the tool is part of the directly-exposed
	// surface (tools/list) in addition to progressive discovery. The curated
	// registration loop registers every DirectVisible entry; the search/describe
	// meta-tools index the whole catalog regardless.
	DirectVisible bool
	// Interaction tells agents whether this tool is safe to invoke directly,
	// prompts interactively, or reads piped stdin. It is set at registration
	// time (e.g. the compiled surface's Operation metadata or the OOB setup
	// handlers).
	Interaction Interaction
	InputSchema json.RawMessage
	// OutputSchema is the JSON Schema describing the tool's StructuredContent
	// (the shape of a successful CallToolResult). The wire seam emits it as the
	// official tool's `outputSchema` so the descriptor matches what the handler
	// actually returns. When nil, no outputSchema is declared on the wire.
	OutputSchema json.RawMessage
	// Meta carries arbitrary tool metadata (e.g. MCP Apps `_meta.ui`) through
	// curated registration. SDK-neutral; the wire seam encodes it onto the
	// tool. Extended, never replaced, when attaching app metadata.
	Meta map[string]any
	// SecuritySchemes declares the tool's auth policy for OpenAI/ChatGPT
	// per-tool `securitySchemes`. Nil means the wire seam applies the server
	// default (oauth2, no scopes).
	SecuritySchemes []SecurityScheme
	// SensitiveFlags lists the long flag names whose values are credential
	// material and must be redacted from the in-process arg-trace log. It is
	// derived from the command's flag declarations (SensitiveProvider) at
	// registration time, so the redaction vocabulary cannot drift from the
	// CLI.
	SensitiveFlags []string
	// MCPTargets carries profile-keyed presentation variants for the MCP
	// surface. When non-nil, the catalog resolves the best-matching target
	// per request using the detected platform profile, overriding the static
	// Description. When nil, the static Description is used. Only set on
	// custom transport tools (upload_file, vault_put_file, download_file)
	// whose agent-facing description varies by host environment.
	MCPTargets []ToolTarget
	Handler    PinnerToolHandler
}
